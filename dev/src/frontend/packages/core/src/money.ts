import { type Price, type Rate, type Spec, safeInteger } from './contracts';
/** Presentation estimates only. Server quotes, measured usage and final channel bills remain authoritative. */
export function yuan(fen: number | bigint): string {
  const n = typeof fen === 'bigint' ? fen : BigInt(safeInteger(fen));
  if (n < 0n) throw new Error('INVALID_AMOUNT');
  return '¥' + (n / 100n).toString() + '.' + (n % 100n).toString().padStart(2, '0');
}
export function parseYuan(input: string): number {
  if (!/^(0|[1-9]\d{0,13})(\.\d{1,2})?$/.test(input)) throw new Error('金额请填写最多两位小数的人民币数值');
  const [a, b = ''] = input.split('.'); const n = BigInt(a) * 100n + BigInt(b.padEnd(2, '0'));
  if (n > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('金额超出支持范围');
  return Number(n);
}
export function rateText(rate: Rate): string {
  const n = BigInt(safeInteger(rate.numerator_fen)), d = BigInt(safeInteger(rate.denominator_units, 1)) * 100n;
  const scale = 1_000_000n, raw = n * scale / d, exact = n * scale % d === 0n;
  if (n > 0n && raw === 0n) return '< ¥0.000001';
  let decimal = (raw % scale).toString().padStart(6, '0').replace(/0+$/, '');
  if (decimal.length < 2) decimal = decimal.padEnd(2, '0');
  return (exact ? '' : '约 ') + '¥' + (raw / scale).toString() + '.' + decimal;
}
export function estimate(price: Price, spec: Spec): number {
  const part = (count: number | undefined, rate: Rate | undefined, basis: bigint): [bigint, bigint] => {
    if (!rate) throw new Error('费率待确认');
    return [BigInt(safeInteger(count)) * BigInt(safeInteger(rate.numerator_fen)), BigInt(safeInteger(rate.denominator_units, 1)) * basis];
  };
  let n: bigint, d: bigint;
  if (price.unit === 'AVAILABLE_HOUR') [n, d] = part(spec.duration_ms, price.hour_rate, 3_600_000n);
  else {
    const [a, b] = part(spec.input_token_limit, price.input_rate, 1_000_000n), [c, e] = part(spec.output_token_limit, price.output_rate, 1_000_000n);
    n = a * e + c * b; d = b * e;
  }
  const result = (n + d - 1n) / d;
  if (result > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('预估费用超出范围');
  return Number(result);
}
export function splitIncludedFee(gross: number, bps: number): { commission: number; seller: number } {
  safeInteger(gross); safeInteger(bps); if (bps > 10000) throw new Error('INVALID_FEE');
  const fee = BigInt(gross) * BigInt(bps) / 10000n;
  return { commission: Number(fee), seller: gross - Number(fee) };
}
