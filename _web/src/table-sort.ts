/** Sorting is local to the loaded rows; callers retain their original results. */
export type SortDirection = "ascending" | "descending";
export function sortableHeaders(headers: HTMLElement[], onSort: (index: number, direction: SortDirection) => void): void {
  let active = -1;
  let direction: SortDirection = "ascending";
  headers.forEach((header, index) => {
    header.classList.add("sortable-header");
    header.tabIndex = 0;
    header.setAttribute("aria-sort", "none");
    const arrow = document.createElement("span");
    arrow.className = "sort-indicator";
    arrow.setAttribute("aria-hidden", "true");
    header.append(arrow);
    function sort(): void {
      direction = active === index && direction === "ascending" ? "descending" : "ascending";
      active = index;
      headers.forEach((item, i) => item.setAttribute("aria-sort", i === index ? direction : "none"));
      onSort(index, direction);
    }
    header.addEventListener("click", sort);
    header.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") { event.preventDefault(); sort(); }
    });
  });
}

/** Compare decimal/scientific strings without rounding large integers to Number. */
function decimal(value: string): { sign: number; power: number; digits: string } | null {
  const match = /^([+-]?)(\d*)(?:\.(\d*))?(?:e([+-]?\d+))?$/i.exec(value.trim());
  if (!match || !(match[2] || match[3])) return null;
  const raw = (match[2] || "") + (match[3] || "");
  const digits = raw.replace(/^0+/, "");
  const power = digits.length - (match[3]?.length || 0) + Number(match[4] || 0);
  if (!Number.isSafeInteger(power)) return null;
  return { sign: digits ? match[1] === "-" ? -1 : 1 : 0, power, digits };
}
export function compareValues(a: string | number | boolean | null | undefined, b: string | number | boolean | null | undefined, numeric = false): number {
  if (a == null || b == null) return a == null ? b == null ? 0 : 1 : -1;
  if (numeric) {
    const left = decimal(String(a)); const right = decimal(String(b));
    if (left && right) {
      if (left.sign !== right.sign) return left.sign - right.sign;
      if (!left.sign) return 0;
      if (left.power !== right.power) return Math.sign(left.power - right.power) * left.sign;
      const length = Math.max(left.digits.length, right.digits.length);
      const x = left.digits.padEnd(length, "0"); const y = right.digits.padEnd(length, "0");
      return (x < y ? -1 : x > y ? 1 : 0) * left.sign;
    }
  }
  if (typeof a === "boolean" && typeof b === "boolean") return Number(a) - Number(b);
  return String(a).localeCompare(String(b));
}
