// §5.5 auth handshake: neither EventSource nor <iframe> can set request
// headers, so `--token` mode relies on a cookie. On startup, if the URL
// carries ?token=, hit any API once so the server can set the HttpOnly
// cookie, then strip the token from the address bar.

export async function bootstrapAuth(): Promise<void> {
  const url = new URL(window.location.href);
  const token = url.searchParams.get('token');
  if (!token) return;

  try {
    await fetch(`/api/health?token=${encodeURIComponent(token)}`, {
      credentials: 'same-origin',
    });
  } finally {
    url.searchParams.delete('token');
    const rest = `${url.pathname}${url.search}${url.hash}`;
    window.history.replaceState(null, '', rest);
  }
}
