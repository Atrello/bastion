// Loads something from the API for a page: its data, any error, and reload() for after a change.

export function load<T>(fetcher: () => Promise<T>) {
  let data = $state<T>();
  let error = $state<string>();
  let loading = $state(true);

  async function reload() {
    loading = true;
    try {
      data = await fetcher();
      error = undefined;
    } catch (err) {
      error = (err as Error).message;
    } finally {
      loading = false;
    }
  }
  void reload();

  return {
    get data() {
      return data;
    },
    get error() {
      return error;
    },
    get loading() {
      return loading;
    },
    reload,
  };
}
