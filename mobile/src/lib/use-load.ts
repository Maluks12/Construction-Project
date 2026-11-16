import { useFocusEffect } from "expo-router";
import { useCallback, useEffect, useRef, useState } from "react";

// Loads data when a screen comes into view, and again each time it's
// returned to, so a list is fresh after saving something on another screen.
// `deps` are the values the load depends on (the site id, a record id): it
// runs again when any of them changes.
export function useLoad<T>(load: () => Promise<T>, deps: (string | undefined)[]) {
  const [state, setState] = useState<{ data?: T; error?: string; loading: boolean }>({ loading: true });
  const latest = useRef(0);
  const loader = useRef(load);
  const key = deps.join("|");

  // Always call the newest `load`, which closes over the current props.
  useEffect(() => {
    loader.current = load;
  });

  const run = useCallback(async () => {
    const call = ++latest.current;
    setState((s) => ({ ...s, loading: true, error: undefined }));
    try {
      const data = await loader.current();
      // A slower, older call must not overwrite a newer one.
      if (call === latest.current) setState({ data, loading: false });
    } catch (e) {
      if (call === latest.current) {
        setState((s) => ({ ...s, loading: false, error: e instanceof Error ? e.message : "Something went wrong." }));
      }
    }
  }, []);

  useFocusEffect(
    useCallback(() => {
      void run();
      // `key` isn't read here; it's listed so a changed dep reloads.
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [run, key]),
  );

  return { ...state, reload: run };
}

export const message = (e: unknown) => (e instanceof Error ? e.message : "Something went wrong.");
