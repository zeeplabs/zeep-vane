import { useId, useState } from "react";
// Imported from the package's deep subpaths, not the root "@zeeptech/toolkit"
// barrel: the barrel is CommonJS and re-exports every module (including
// utils/brazilian-cities.js, ~180KB uncompressed, unrelated to phone
// masking), which Rollup can't tree-shake out of a `require()` re-export -
// see AD-016. Deep-importing only masks/ (which itself needs
// utils/countries) avoids pulling that in.
import { globalCellphoneMask } from "@zeeptech/toolkit/dist/masks";
import { countries } from "@zeeptech/toolkit/dist/utils/countries";
import { Input, inputVariantClasses, type InputVariant } from "./Input";

export interface PhoneFieldProps {
  label: string;
  /** Receives the full phone number already formatted with the dial code (e.g.: "+55 (11) 98765-4321"), or "" if the field is empty. */
  onChange: (value: string) => void;
  required?: boolean;
  /** Same semantics as Input/Field - "filled" in drawers already migrated
   * (new-layout-migration), "default" (bordered) in screens not yet
   * migrated (e.g. BootstrapPage). Without this, the dial code/phone
   * fields would visually clash with the rest of the form even when the
   * surrounding form already used filled. */
  variant?: InputVariant;
}

const DEFAULT_COUNTRY_CODE = "BR";

// PhoneField: country (dial code) selector + input with local masking, since
// Vane is used by companies outside Brazil (AD-002 is about tenancy, not
// geography) - a phone field hardcoded to a Brazilian area code would
// exclude any international installation. globalCellphoneMask/countries
// come from @zeeptech/toolkit (AD-016) instead of reimplementing 200
// national masks here.
export function PhoneField({ label, onChange, required, variant = "default" }: PhoneFieldProps) {
  const inputId = useId();
  const [countryCode, setCountryCode] = useState(DEFAULT_COUNTRY_CODE);
  const [localValue, setLocalValue] = useState("");

  const selected = countries.find((c) => c.code === countryCode) ?? countries[0];

  function emit(nextCountryCode: string, nextLocalValue: string) {
    const country = countries.find((c) => c.code === nextCountryCode);
    const masked = globalCellphoneMask(nextCountryCode, nextLocalValue);
    onChange(nextLocalValue ? `${country?.dialCode ?? ""} ${masked}`.trim() : "");
  }

  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={inputId} className="text-sm font-medium text-text">
        {label}
      </label>
      <div className="flex gap-2">
        <select
          aria-label={`${label} - DDI`}
          value={countryCode}
          onChange={(e) => {
            setCountryCode(e.target.value);
            setLocalValue("");
            emit(e.target.value, "");
          }}
          className={
            "min-h-9 w-[104px] flex-none rounded-md border px-2 text-sm text-text outline-none transition-colors " +
            inputVariantClasses[variant]
          }
        >
          {countries.map((c) => (
            <option key={c.code} value={c.code}>
              {c.flag} {c.dialCode}
            </option>
          ))}
        </select>
        <Input
          id={inputId}
          type="tel"
          variant={variant}
          autoComplete="tel-national"
          value={globalCellphoneMask(countryCode, localValue)}
          placeholder={selected.mask.replace(/9/g, "0")}
          required={required}
          onChange={(e) => {
            const digits = e.target.value.replace(/\D/g, "");
            setLocalValue(digits);
            emit(countryCode, digits);
          }}
          className="flex-1"
        />
      </div>
    </div>
  );
}
