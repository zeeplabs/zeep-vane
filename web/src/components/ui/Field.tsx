import { type ReactNode, useId } from "react";
import { Input, type InputProps } from "./Input";

export interface FieldProps extends InputProps {
  label: string;
  error?: string;
  hint?: ReactNode;
}

export function Field({ label, error, hint, id, ...inputProps }: FieldProps) {
  const generatedId = useId();
  const inputId = id ?? generatedId;

  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={inputId} className="text-sm font-medium text-text">
        {label}
      </label>
      <Input id={inputId} {...inputProps} />
      {error ? (
        <p role="alert" className="text-xs text-critical">
          {error}
        </p>
      ) : hint ? (
        <p className="text-xs text-neutral-400">{hint}</p>
      ) : null}
    </div>
  );
}
