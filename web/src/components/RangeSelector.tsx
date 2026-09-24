import { RANGES, type RangeKey } from "../types/api";

export function RangeSelector({ value, onChange }: { value: RangeKey; onChange: (r: RangeKey) => void }) {
  return (
    <div className="segmented" role="radiogroup" aria-label="Time range">
      {RANGES.map((r) => (
        <button key={r} role="radio" aria-checked={r === value} className={r === value ? "active" : ""} onClick={() => onChange(r)}>
          {r}
        </button>
      ))}
    </div>
  );
}
