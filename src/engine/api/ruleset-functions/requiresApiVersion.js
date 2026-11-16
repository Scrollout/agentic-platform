import { params, METHODS } from './params.js';

export default function requiresApiVersion(pathItem, _opts, ctx) {
  const path = ctx.path[ctx.path.length - 1];
  if (String(path).startsWith('/providers/')) return [];
  const doc = ctx.document.data;
  const errors = [];
  for (const m of METHODS) {
    const op = pathItem[m];
    if (!op) continue;
    const ok = params(pathItem, op, doc).some((p) => p.name === 'api-version' && p.in === 'query' && p.required === true);
    if (!ok) errors.push({ message: `${m.toUpperCase()} ${path} must require the api-version query parameter.`, path: [...ctx.path, m] });
  }
  return errors;
}
