import { useEffect, useLayoutEffect, useRef, useState } from 'react';

export const POLL_MS = 15000;

export interface Polled<T> {
  data?: T;
  error?: string;
  status?: number;
  // The data on screen is from an earlier url: hold it dimmed until the new one arrives.
  stale: boolean;
}

// usePoll loads url now, every POLL_MS and whenever tick changes.
export function usePoll<T>(url: string, tick: number): Polled<T> {
  const [state, setState] = useState<{ data?: T; from?: string; error?: string; status?: number }>({});
  useEffect(() => {
    const ctl = new AbortController();
    const load = async () => {
      try {
        const res = await fetch(url, { cache: 'no-store', signal: ctl.signal });
        const body = await res.json();
        if (res.ok) setState({ data: body, from: url });
        else setState(s => ({ ...s, error: body.error || res.statusText, status: res.status }));
      } catch {
        if (!ctl.signal.aborted) setState(s => ({ ...s, error: "can't reach ccmeter", status: undefined }));
      }
    };
    load();
    const timer = setInterval(load, POLL_MS);
    return () => {
      ctl.abort();
      clearInterval(timer);
    };
  }, [url, tick]);
  return { data: state.data, error: state.error, status: state.status, stale: state.from !== url };
}

// useWidth measures an element, so charts can draw in real pixels.
export function useWidth<E extends Element>() {
  const ref = useRef<E>(null);
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    setWidth(el.getBoundingClientRect().width);
    const ro = new ResizeObserver(([e]) => setWidth(e.contentRect.width));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, width] as const;
}

export function useTitle(title: string) {
  useEffect(() => {
    document.title = title;
  }, [title]);
}
