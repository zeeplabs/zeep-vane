import { forwardRef, type InputHTMLAttributes } from "react";

export type InputProps = InputHTMLAttributes<HTMLInputElement>;

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { className = "", ...props },
  ref
) {
  return (
    <input
      ref={ref}
      className={
        "w-full min-h-9 rounded-md border border-divider bg-surface text-text px-3 text-sm " +
        "outline-none transition-colors focus:border-accent " +
        className
      }
      {...props}
    />
  );
});
