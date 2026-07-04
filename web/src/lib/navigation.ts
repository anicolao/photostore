export function objectURLWithContext(viewURL: string, context: Record<string, string>) {
  const params = new URLSearchParams(context);
  const query = params.toString();
  return query ? `${viewURL}?${query}` : viewURL;
}
