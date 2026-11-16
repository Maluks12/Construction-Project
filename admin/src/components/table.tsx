import type { ReactNode } from "react";

// A plain table from a list of headings and rows of cells. Most admin
// screens are registers, so this keeps each page down to its data.
export function Table({ head, rows }: { head: string[]; rows: ReactNode[][] }) {
  return (
    <div className="overflow-x-auto">
      <table className="tbl">
        <thead>
          <tr>{head.map((h, i) => <th key={i}>{h}</th>)}</tr>
        </thead>
        <tbody>
          {rows.map((cells, r) => (
            <tr key={r}>{cells.map((c, i) => <td key={i}>{c}</td>)}</tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// Bold first line with a small grey line under it, as in the dashboard.
export function Two({ top, sub }: { top: ReactNode; sub?: ReactNode }) {
  return (
    <>
      <strong className="font-semibold">{top}</strong>
      {sub && <div className="text-xs text-muted">{sub}</div>}
    </>
  );
}
