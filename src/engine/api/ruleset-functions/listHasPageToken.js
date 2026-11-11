import { params } from './params.js';

export default function listHasPageToken(pathItem, _opts, ctx) {
  const op = pathItem.get;
  if (!op) return [];
  const doc = ctx.document.data;
  const content = (((op.responses || {})['200'] || {}).content || {})['application/json'] || {};
  let schema = content.schema || {};
  if (schema.$ref && schema.$ref.startsWith('#/')) {
    schema = schema.$ref.slice(2).split('/').reduce((o, k) => (o ? o[k] : undefined), doc) || {};
  }
  const items = (schema.properties || {}).items;
  if (!items || items.type !== 'array') return [];
  const ok = params(pathItem, op, doc).some((p) => p.name === 'page_token' && p.in === 'query');
  const path = ctx.path[ctx.path.length - 1];
  return ok ? [] : [{ message: `GET ${path} returns a list and must accept page_token.`, path: [...ctx.path, 'get'] }];
}
