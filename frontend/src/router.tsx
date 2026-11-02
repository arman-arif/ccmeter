import { useSyncExternalStore, type AnchorHTMLAttributes, type MouseEvent } from 'react';

// A small history router: the dashboard only has a handful of routes, so this
// saves pulling in a routing library. server.go answers every page path with
// index.html, so links and reloads work on any route.

const listeners = new Set<() => void>();
const emit = () => listeners.forEach(l => l());
window.addEventListener('popstate', emit);

function subscribe(l: () => void) {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

export function navigate(to: string, opts: { replace?: boolean; keepScroll?: boolean } = {}) {
  if (opts.replace) history.replaceState(null, '', to);
  else history.pushState(null, '', to);
  if (!opts.keepScroll) window.scrollTo(0, 0);
  emit();
}

export function useUrl(): URL {
  const href = useSyncExternalStore(subscribe, () => location.href);
  return new URL(href);
}

export type Route = { page: 'overview' } | { page: 'account'; name: string } | { page: 'missing' };

export function matchRoute(pathname: string): Route {
  if (pathname === '/') return { page: 'overview' };
  const m = pathname.match(/^\/accounts\/([^/]+)\/?$/);
  if (m) {
    try {
      return { page: 'account', name: decodeURIComponent(m[1]) };
    } catch {
      return { page: 'missing' };
    }
  }
  return { page: 'missing' };
}

export const accountPath = (name: string) => '/accounts/' + encodeURIComponent(name);

// A normal anchor, so opening in a new tab and copying the link still work.
export function Link({ href, onClick, ...rest }: AnchorHTMLAttributes<HTMLAnchorElement> & { href: string }) {
  const handle = (e: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(e);
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    navigate(href);
  };
  return <a href={href} onClick={handle} {...rest} />;
}
