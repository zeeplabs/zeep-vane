import { useId, useState } from "react";
// Imported from the package's deep subpaths, not the root "@zeeptech/toolkit"
// barrel: the barrel is CommonJS and re-exports every module (including
// utils/brazilian-cities.js, ~180KB uncompressed, unrelated to phone
// masking), which Rollup can't tree-shake out of a `require()` re-export -
// see AD-016. Deep-importing only masks/ (which itself needs
// utils/countries) avoids pulling that in.
import { globalCellphoneMask } from "@zeeptech/toolkit/dist/masks";
import { countries } from "@zeeptech/toolkit/dist/utils/countries";
import { Input } from "./Input";

export interface PhoneFieldProps {
  label: string;
  /** Recebe o telefone completo já formatado com DDI (ex.: "+55 (11) 98765-4321"), ou "" se o campo estiver vazio. */
  onChange: (value: string) => void;
  required?: boolean;
}

const DEFAULT_COUNTRY_CODE = "BR";

// PhoneField: seletor de país (DDI) + input com máscara local, já que o
// Vane é usado por empresas fora do Brasil (AD-002 é sobre tenancy, não
// sobre geografia) - um celular só com DDD brasileiro fixo excluiria
// qualquer instalação internacional. globalCellphoneMask/countries vêm do
// @zeeptech/toolkit (AD-016) em vez de reimplementar 200 máscaras nacionais
// aqui.
export function PhoneField({ label, onChange, required }: PhoneFieldProps) {
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
          className="min-h-9 w-[104px] flex-none rounded-md border border-divider bg-surface px-2 text-sm text-text outline-none transition-colors focus:border-accent"
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
