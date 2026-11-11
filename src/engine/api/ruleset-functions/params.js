// Shared helper: resolve an operation's parameters, following local $refs.
export function params(pathItem, op, document) {
  const resolve = (p) => {
    if (p && p.$ref && p.$ref.startsWith('#/')) {
      return p.$ref.slice(2).split('/').reduce((o, k) => (o ? o[k] : undefined), document);
    }
    return p;
  };
  return [...(pathItem.parameters || []), ...(op.parameters || [])].map(resolve).filter(Boolean);
}
export const METHODS = ['get', 'put', 'post', 'delete', 'patch', 'head', 'options'];
