// Shared shape for context-menu entries. It lives outside the SFC so both the
// menu component and its callers can import the type (Vue's <script setup> does
// not export named types).
export interface MenuAction {
  label: string;
  icon?: string;
  hint?: string;
  danger?: boolean;
  disabled?: boolean;
  separator?: boolean;
  action?: () => void;
}
