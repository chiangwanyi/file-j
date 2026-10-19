// Package transfer implements the queue that moves bytes between any two
// fsx.Provider implementations. Because the local disk and an SFTP session both
// satisfy the same interface, copy/move/upload/download are one algorithm:
//
//	local  -> local   copy / move
//	local  -> remote  upload  (copy with a remote destination)
//	remote -> local   download
//	remote -> remote  not supported directly (would require a server-side copy)
package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"
	"time"

	"github.com/jiangwanyi/file-j/backend/internal/config"
	"github.com/jiangwanyi/file-j/backend/internal/fsx"
)

// Endpoint identifies one side of a transfer.
type Endpoint struct {
	Side      string `json:"side"` // local | remote
	SessionID string `json:"sessionId,omitempty"`
	Path      string `json:"path"`
}

// Item is one requested transfer: copy/move From.Path into the folder To.Path.
type Item struct {
	From Endpoint `json:"from"`
	To   Endpoint `json:"to"`
	// Name overrides the destination basename (used by "save as"/rename-on-copy).
	Name string `json:"name,omitempty"`
}

// CreateRequest is the payload of the transfer.create call.
type CreateRequest struct {
	Op       string `json:"op"`       // copy | move
	Conflict string `json:"conflict"` // overwrite | skip | keepboth | fail
	Items    []Item `json:"items"`
	// Optional per-request overrides of the persisted defaults.
	PreserveTimes       *bool `json:"preserveTimes,omitempty"`
	PreservePermissions *bool `json:"preservePermissions,omitempty"`
}

// Task status values.
const (
	StatusQueued   = "queued"
	StatusRunning  = "running"
	StatusDone     = "done"
	StatusFailed   = "failed"
	StatusCanceled = "canceled"
	StatusSkipped  = "skipped"
)

// Task is the public, JSON-serialisable view of a queued transfer.
type Task struct {
	ID        string   `json:"id"`
	Op        string   `json:"op"`        // copy | move
	Direction string   `json:"direction"` // local-local | upload | download
	Name      string   `json:"name"`
	From      Endpoint `json:"from"`
	To        Endpoint `json:"to"`
	DestPath  string   `json:"destPath"`

	Status     string     `json:"status"`
	Bytes      int64      `json:"bytes"`
	TotalBytes int64      `json:"totalBytes"`
	Files      int        `json:"files"`
	FilesDone  int        `json:"filesDone"`
	Speed      float64    `json:"speed"`
	Message    string     `json:"message,omitempty"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`

	cancel    context.CancelFunc
	conflict  string
	overrides *overrides
}

// ResolveFunc turns an endpoint into a provider, dialling SSH if necessary.
type ResolveFunc func(ctx context.Context, ep Endpoint) (fsx.Provider, error)

// EmitFunc publishes an event to every connected UI.
type EmitFunc func(event string, payload any)

// Engine is the transfer queue.
type Engine struct {
	resolve ResolveFunc
	emit    EmitFunc
	cfg     func() config.Config

	ctx    context.Context
	cancel context.CancelFunc
	wake   chan struct{}

	mu      sync.Mutex
	tasks   map[string]*Task
	order   []string
	running int
	limit   int
	closed  bool
}

// New creates an engine. cfg is read lazily so settings changes apply to the
// next task without recreating the engine.
func New(resolve ResolveFunc, emit EmitFunc, cfg func() config.Config) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{
		resolve: resolve,
		emit:    emit,
		cfg:     cfg,
		ctx:     ctx,
		cancel:  cancel,
		wake:    make(chan struct{}, 1),
		tasks:   map[string]*Task{},
		limit:   cfg().Transfer.Concurrency,
	}
	go e.dispatch()
	return e
}

// Create enqueues one task per item and returns them immediately.
func (e *Engine) Create(req CreateRequest) ([]Task, error) {
	if len(req.Items) == 0 {
		return nil, errors.New("没有要传输的项目")
	}
	if req.Op != "copy" && req.Op != "move" {
		return nil, fmt.Errorf("不支持的传输操作: %s", req.Op)
	}
	cfg := e.cfg()
	switch req.Conflict {
	case "":
		req.Conflict = cfg.Transfer.Conflict
	case "overwrite", "skip", "keepboth", "fail":
	default:
		return nil, fmt.Errorf("未知的冲突策略: %s", req.Conflict)
	}

	// Validate everything up front so a bad item cannot leave half a batch queued.
	for _, item := range req.Items {
		if item.From.Path == "" || item.To.Path == "" {
			return nil, errors.New("源路径或目标路径为空")
		}
		if item.From.Side == "remote" && item.To.Side == "remote" {
			return nil, errors.New("暂不支持远程到远程的直接传输")
		}
	}

	out := make([]Task, 0, len(req.Items))
	e.mu.Lock()
	for _, item := range req.Items {
		name := item.Name
		if name == "" {
			name = fsx.Base(item.From.Path)
		}
		t := &Task{
			ID:        newID(),
			Op:        req.Op,
			Direction: direction(item.From.Side, item.To.Side),
			Name:      name,
			From:      item.From,
			To:        item.To,
			DestPath:  fsx.Join(item.To.Path, name),
			Status:    StatusQueued,
			CreatedAt: time.Now(),
			conflict:  req.Conflict,
		}
		if req.PreserveTimes != nil || req.PreservePermissions != nil {
			// Overrides are stored on the task so concurrent settings changes
			// cannot alter a task that is already queued.
			t.overrides = &overrides{times: req.PreserveTimes, perms: req.PreservePermissions}
		}
		e.tasks[t.ID] = t
		e.order = append(e.order, t.ID)
		out = append(out, t.clone())
	}
	e.trimLocked()
	e.mu.Unlock()

	for _, t := range out {
		e.publish("transfer.task", map[string]any{"task": t})
	}
	e.poke()
	return out, nil
}

// List returns all tasks, newest first.
func (e *Engine) List() []Task {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Task, 0, len(e.order))
	for i := len(e.order) - 1; i >= 0; i-- {
		if t, ok := e.tasks[e.order[i]]; ok {
			out = append(out, t.clone())
		}
	}
	return out
}

// Cancel aborts a queued or running task.
func (e *Engine) Cancel(id string) error {
	e.mu.Lock()
	t, ok := e.tasks[id]
	e.mu.Unlock()
	if !ok {
		return errors.New("任务不存在")
	}
	if t.cancel != nil {
		t.cancel()
	}
	var snapshot *Task
	e.mu.Lock()
	if t.Status == StatusQueued {
		now := time.Now()
		t.Status = StatusCanceled
		t.FinishedAt = &now
		c := t.clone()
		snapshot = &c
	}
	e.mu.Unlock()
	if snapshot != nil {
		e.publish("transfer.task", map[string]any{"task": *snapshot})
	}
	return nil
}

// Remove deletes a finished task from the list.
func (e *Engine) Remove(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, ok := e.tasks[id]
	if !ok {
		return
	}
	if t.Status == StatusRunning || t.Status == StatusQueued {
		return
	}
	delete(e.tasks, id)
	for i, tid := range e.order {
		if tid == id {
			e.order = append(e.order[:i], e.order[i+1:]...)
			break
		}
	}
}

// ClearFinished removes every finished task.
func (e *Engine) ClearFinished() {
	e.mu.Lock()
	defer e.mu.Unlock()
	kept := e.order[:0]
	for _, id := range e.order {
		t := e.tasks[id]
		if t == nil {
			continue
		}
		if t.Status == StatusRunning || t.Status == StatusQueued {
			kept = append(kept, id)
			continue
		}
		delete(e.tasks, id)
	}
	e.order = kept
}

// Retry re-enqueues a finished task under a new ID.
func (e *Engine) Retry(id string) (Task, error) {
	e.mu.Lock()
	t, ok := e.tasks[id]
	if !ok {
		e.mu.Unlock()
		return Task{}, errors.New("任务不存在")
	}
	if t.Status == StatusRunning || t.Status == StatusQueued {
		e.mu.Unlock()
		return Task{}, errors.New("任务尚未结束")
	}
	req := CreateRequest{
		Op:       t.Op,
		Conflict: t.conflict,
		Items:    []Item{{From: t.From, To: t.To, Name: t.Name}},
	}
	e.mu.Unlock()
	tasks, err := e.Create(req)
	if err != nil {
		return Task{}, err
	}
	return tasks[0], nil
}

// Close stops the dispatcher and cancels everything in flight.
func (e *Engine) Close() {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.closed = true
	tasks := make([]*Task, 0, len(e.tasks))
	for _, t := range e.tasks {
		tasks = append(tasks, t)
	}
	e.mu.Unlock()
	e.cancel()
	for _, t := range tasks {
		if t.cancel != nil {
			t.cancel()
		}
	}
}

func (e *Engine) poke() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) dispatch() {
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-e.wake:
		}
		for {
			e.mu.Lock()
			if e.closed {
				e.mu.Unlock()
				return
			}
			e.limit = e.cfg().Transfer.Concurrency
			if e.running >= e.limit {
				e.mu.Unlock()
				break
			}
			t := e.nextQueuedLocked()
			if t == nil {
				e.mu.Unlock()
				break
			}
			ctx, cancel := context.WithCancel(e.ctx)
			t.cancel = cancel
			t.Status = StatusRunning
			now := time.Now()
			t.StartedAt = &now
			e.running++
			snapshot := t.clone()
			e.mu.Unlock()

			e.publish("transfer.task", map[string]any{"task": snapshot})
			go func() {
				defer func() {
					cancel()
					e.mu.Lock()
					e.running--
					e.mu.Unlock()
					e.poke()
				}()
				e.run(ctx, t)
			}()
		}
	}
}

func (e *Engine) nextQueuedLocked() *Task {
	for _, id := range e.order {
		if t := e.tasks[id]; t != nil && t.Status == StatusQueued {
			return t
		}
	}
	return nil
}

// trimLocked caps the history so a long session cannot grow memory without bound.
func (e *Engine) trimLocked() {
	const maxTasks = 200
	for len(e.order) > maxTasks {
		removed := false
		for i, id := range e.order {
			t := e.tasks[id]
			if t != nil && (t.Status == StatusRunning || t.Status == StatusQueued) {
				continue
			}
			delete(e.tasks, id)
			e.order = append(e.order[:i], e.order[i+1:]...)
			removed = true
			break
		}
		if !removed {
			return
		}
	}
}

func (e *Engine) publish(event string, payload any) {
	if e.emit != nil {
		e.emit(event, payload)
	}
}

func (e *Engine) patch(id string, fn func(*Task)) Task {
	e.mu.Lock()
	t := e.tasks[id]
	if t == nil {
		e.mu.Unlock()
		return Task{}
	}
	fn(t)
	snapshot := t.clone()
	e.mu.Unlock()
	e.publish("transfer.task", map[string]any{"task": snapshot})
	return snapshot
}

func (e *Engine) applyMeta(ctx context.Context, p fsx.Provider, path string, entry fsx.Entry, ov *overrides) {
	cfg := e.cfg().Transfer
	preserveTimes := cfg.PreserveTimes
	preservePerms := cfg.PreservePermissions
	if ov != nil {
		if ov.times != nil {
			preserveTimes = *ov.times
		}
		if ov.perms != nil {
			preservePerms = *ov.perms
		}
	}
	if preservePerms {
		if mode, ok := parseMode(entry.Mode); ok {
			_ = p.Chmod(ctx, path, mode)
		}
	}
	if preserveTimes {
		_ = p.Chtimes(ctx, path, entry.ModTime, entry.ModTime)
	}
}

// run executes one task end-to-end.
func (e *Engine) run(ctx context.Context, t *Task) {
	cfg := e.cfg()
	start := time.Now()

	src, err := e.resolve(ctx, t.From)
	if err != nil {
		e.fail(t.ID, err)
		return
	}
	dst, err := e.resolve(ctx, t.To)
	if err != nil {
		e.fail(t.ID, err)
		return
	}

	entry, err := src.Stat(ctx, t.From.Path)
	if err != nil {
		e.fail(t.ID, fmt.Errorf("读取源文件失败: %w", err))
		return
	}
	// The destination folder must exist before any bytes move. Doing it here
	// rather than in the UI keeps drag-and-drop into a stale folder working.
	if err := dst.MkdirAll(ctx, t.To.Path, 0o755); err != nil {
		e.fail(t.ID, fmt.Errorf("创建目标文件夹失败: %w", err))
		return
	}

	name := t.Name
	destRoot := fsx.Join(t.To.Path, name)

	// Fast path: a same-provider move is a rename, no bytes are copied.
	if t.Op == "move" && sameProvider(src, dst) {
		if err := dst.Rename(ctx, t.From.Path, destRoot); err == nil {
			e.complete(t.ID, 0, 1)
			return
		}
		// Different volume or permission issue: fall through to copy+delete.
	}

	destRoot, err = e.resolveRootConflict(ctx, dst, destRoot, t.conflict)
	if err != nil {
		if errors.Is(err, errSkip) {
			e.patch(t.ID, func(tk *Task) {
				now := time.Now()
				tk.Status = StatusSkipped
				tk.Message = "目标已存在，已跳过"
				tk.FinishedAt = &now
			})
			return
		}
		e.fail(t.ID, err)
		return
	}

	// Pre-scan supplies a determinate progress bar. It is bounded so a huge tree
	// degrades to an indeterminate bar instead of stalling the queue.
	total, files, err := scanTotal(ctx, src, t.From.Path, entry)
	if err != nil {
		e.fail(t.ID, err)
		return
	}
	e.patch(t.ID, func(tk *Task) {
		tk.TotalBytes = total
		tk.Files = files
		tk.DestPath = destRoot
	})

	state := &runState{}
	stopProgress := make(chan struct{})
	go func() {
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		var lastBytes int64
		var lastAt = start
		for {
			select {
			case <-stopProgress:
				return
			case now := <-ticker.C:
				b := state.bytes.Load()
				delta := b - lastBytes
				elapsed := now.Sub(lastAt).Seconds()
				speed := 0.0
				if elapsed > 0 {
					speed = float64(delta) / elapsed
				}
				lastBytes, lastAt = b, now
				d := state.filesDone.Load()
				e.patch(t.ID, func(tk *Task) {
					tk.Bytes = b
					tk.FilesDone = int(d)
					tk.Speed = speed
				})
			}
		}
	}()

	job := &job{
		engine:   e,
		src:      src,
		dst:      dst,
		state:    state,
		conflict: t.conflict,
		over:     t.overrides,
		bufSize:  cfg.Transfer.BufferSizeKB * 1024,
	}
	err = job.copyTree(ctx, t.From.Path, destRoot, entry)
	close(stopProgress)

	if err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			e.patch(t.ID, func(tk *Task) {
				now := time.Now()
				tk.Status = StatusCanceled
				tk.FinishedAt = &now
				tk.Speed = 0
			})
			return
		}
		e.fail(t.ID, err)
		return
	}

	if t.Op == "move" {
		if err := src.Remove(ctx, t.From.Path); err != nil {
			e.fail(t.ID, fmt.Errorf("已复制但删除源文件失败: %w", err))
			return
		}
	}

	e.patch(t.ID, func(tk *Task) {
		now := time.Now()
		tk.Status = StatusDone
		tk.Bytes = state.bytes.Load()
		tk.FilesDone = int(state.filesDone.Load())
		tk.FinishedAt = &now
		tk.Speed = 0
	})
}

func (e *Engine) complete(id string, bytes int64, files int) {
	e.patch(id, func(tk *Task) {
		now := time.Now()
		tk.Status = StatusDone
		tk.Bytes = bytes
		tk.TotalBytes = bytes
		tk.Files = files
		tk.FilesDone = files
		tk.FinishedAt = &now
		tk.Speed = 0
	})
}

func (e *Engine) fail(id string, err error) {
	msg := err.Error()
	e.patch(id, func(tk *Task) {
		now := time.Now()
		tk.Status = StatusFailed
		tk.Error = msg
		tk.FinishedAt = &now
		tk.Speed = 0
	})
}

// resolveRootConflict applies the conflict policy to the top-level destination.
func (e *Engine) resolveRootConflict(ctx context.Context, dst fsx.Provider, dest string, policy string) (string, error) {
	info, err := dst.Stat(ctx, dest)
	if err != nil {
		return dest, nil // does not exist, nothing to resolve
	}
	switch policy {
	case "skip":
		return "", errSkip
	case "keepboth":
		return uniqueName(ctx, dst, dest, info.IsDir)
	case "fail":
		return "", fmt.Errorf("目标已存在: %s", dest)
	default: // overwrite
		return dest, nil
	}
}

var errSkip = errors.New("skip")

func uniqueName(ctx context.Context, p fsx.Provider, dest string, isDir bool) (string, error) {
	base := fsx.Base(dest)
	dir := fsx.Parent(dest)
	stem, ext := base, ""
	if !isDir {
		if i := lastDot(base); i > 0 {
			stem, ext = base[:i], base[i:]
		}
	}
	for i := 1; i < 1000; i++ {
		candidate := fsx.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := p.Stat(ctx, candidate); err != nil {
			return candidate, nil
		}
	}
	return "", errors.New("无法生成不冲突的目标文件名")
}

func lastDot(s string) int {
	for i := len(s) - 1; i > 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

func direction(from, to string) string {
	if from == "local" && to == "remote" {
		return "upload"
	}
	if from == "remote" && to == "local" {
		return "download"
	}
	return "local-local"
}

func sameProvider(a, b fsx.Provider) bool { return a == b }

// scanTotal counts bytes and files, bounded to keep the UI responsive.
func scanTotal(ctx context.Context, p fsx.Provider, root string, entry fsx.Entry) (int64, int, error) {
	const maxEntries = 200000
	if !entry.IsDir {
		return entry.Size, 1, nil
	}
	var bytes int64
	var files int
	visited := 0
	var walk func(path string, e fsx.Entry) error
	walk = func(path string, e fsx.Entry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		visited++
		if visited > maxEntries {
			return nil
		}
		if !e.IsDir {
			bytes += e.Size
			files++
			return nil
		}
		res, err := p.List(ctx, path, true)
		if err != nil {
			return err
		}
		for _, child := range res.Entries {
			if child.IsSymlink {
				continue
			}
			if err := walk(child.Path, child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, entry); err != nil {
		return 0, 0, fmt.Errorf("统计源目录失败: %w", err)
	}
	return bytes, files, nil
}

func parseMode(s string) (fs.FileMode, bool) {
	if len(s) < 10 {
		return 0, false
	}
	var mode fs.FileMode
	const rwx = "rwxrwxrwx"
	for i := 0; i < 9; i++ {
		if s[i+1] == rwx[i] {
			mode |= 1 << uint(8-i)
		}
	}
	if s[3] == 's' || s[3] == 'S' {
		mode |= fs.ModeSetuid
	}
	if s[6] == 's' || s[6] == 'S' {
		mode |= fs.ModeSetgid
	}
	if s[9] == 't' || s[9] == 'T' {
		mode |= fs.ModeSticky
	}
	return mode, true
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// clone returns a copy safe to hand to another goroutine or to JSON.
func (t *Task) clone() Task {
	c := *t
	c.cancel = nil
	return c
}

// overrides holds per-request metadata flags.
type overrides struct {
	times *bool
	perms *bool
}

// runState carries the live counters shared with the progress ticker.
type runState struct {
	bytes     atomicInt64
	filesDone atomicInt64
}

// job is the recursive copy worker for one task.
type job struct {
	engine   *Engine
	src      fsx.Provider
	dst      fsx.Provider
	state    *runState
	conflict string
	over     *overrides
	bufSize  int
}

func (j *job) copyTree(ctx context.Context, from, to string, entry fsx.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if entry.IsSymlink {
		// Symlinks are deliberately not followed: a self-referencing link would
		// make the walk unbounded. They are skipped and reported by the caller.
		return nil
	}
	if entry.IsDir {
		if err := j.dst.MkdirAll(ctx, to, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", to, err)
		}
		list, err := j.src.List(ctx, from, true)
		if err != nil {
			return fmt.Errorf("读取目录 %s 失败: %w", from, err)
		}
		for _, child := range list.Entries {
			if child.IsSymlink {
				continue
			}
			if err := j.copyTree(ctx, child.Path, fsx.Join(to, child.Name), child); err != nil {
				return err
			}
		}
		j.engine.applyMeta(ctx, j.dst, to, entry, j.over)
		return nil
	}

	target := to
	if _, err := j.dst.Stat(ctx, target); err == nil {
		switch j.conflict {
		case "skip":
			return nil
		case "keepboth":
			target, err = uniqueName(ctx, j.dst, target, false)
			if err != nil {
				return err
			}
		case "fail":
			return fmt.Errorf("目标已存在: %s", target)
		}
	}

	if err := j.copyFile(ctx, from, target, entry); err != nil {
		return fmt.Errorf("复制 %s 失败: %w", fsx.Base(from), err)
	}
	j.engine.applyMeta(ctx, j.dst, target, entry, j.over)
	j.state.filesDone.Add(1)
	return nil
}

func (j *job) copyFile(ctx context.Context, from, to string, entry fsx.Entry) error {
	reader, err := j.src.Open(ctx, from)
	if err != nil {
		return err
	}
	defer reader.Close()

	writer, err := j.dst.Create(ctx, to, fs.FileMode(0o644), entry.Size)
	if err != nil {
		return err
	}

	bufSize := j.bufSize
	if bufSize <= 0 {
		bufSize = 128 * 1024
	}
	buf := make([]byte, bufSize)

	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			writer.Close()
			return err
		}
		n, rerr := reader.Read(buf)
		if n > 0 {
			if _, werr := writer.Write(buf[:n]); werr != nil {
				writer.Close()
				return werr
			}
			copied += int64(n)
			j.state.bytes.Add(int64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			writer.Close()
			return rerr
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return nil
}

// atomicInt64 is a tiny wrapper so runState stays readable at call sites.
type atomicInt64 struct {
	mu sync.Mutex
	v  int64
}

func (a *atomicInt64) Add(n int64) {
	a.mu.Lock()
	a.v += n
	a.mu.Unlock()
}

func (a *atomicInt64) Load() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.v
}
