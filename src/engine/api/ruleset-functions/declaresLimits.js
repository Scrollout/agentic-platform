// The document declares a root `x-limits` object holding each key in opts.keys as a positive integer.
export default function declaresLimits(doc, opts) {
  const keys = (opts && opts.keys) || [];
  const limits = doc ? doc['x-limits'] : undefined;
  if (limits === null || typeof limits !== 'object' || Array.isArray(limits)) {
    return [{ message: `The contract must declare a root x-limits object with ${keys.join(', ')}.`, path: ['x-limits'] }];
  }
  const errors = [];
  for (const k of keys) {
    const v = limits[k];
    if (v === undefined) {
      errors.push({ message: `x-limits lacks ${k}.`, path: ['x-limits'] });
    } else if (typeof v !== 'number' || !Number.isInteger(v) || v < 1) {
      errors.push({ message: `x-limits.${k} must be a positive integer, not ${JSON.stringify(v)}.`, path: ['x-limits', k] });
    }
  }
  return errors;
}
