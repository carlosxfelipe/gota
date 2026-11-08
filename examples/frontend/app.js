// @ts-check

/**
 * @typedef {import("../codegen/dist/sdk").TodoResponse} TodoResponse
 * @typedef {import("../codegen/dist/sdk").CreateTodoRequest} CreateTodoRequest
 * @typedef {import("../codegen/dist/sdk").GotaClient} GotaClient
 * @typedef {import("../codegen/dist/sdk").ResponseError<"POST /todos">} PostTodoError
 */

class GotaHttpError extends Error {
  /**
   * @param {number} status
   * @param {string} statusText
   * @param {any} data
   * @param {Headers} [headers]
   */
  constructor(status, statusText, data, headers) {
    const message =
      data && typeof data === "object" && ("message" in data || "error" in data)
        ? String(data.message || data.error)
        : statusText || "HTTP Error " + status;
    super(message);
    this.name = "GotaHttpError";
    this.status = status;
    this.statusText = statusText;
    this.data = data;
    this.headers = headers;
    Object.setPrototypeOf(this, GotaHttpError.prototype);
  }
}

/**
 * Exact runtime implementation of createGotaClient() generated in examples/codegen/dist/sdk.ts
 * @param {string} [baseUrl=""]
 * @param {import("../codegen/dist/sdk").GotaClientOptions} [clientOptions={}]
 * @returns {GotaClient}
 */
function createGotaClient(baseUrl = "", clientOptions = {}) {
  return {
    baseUrl,
    async fetch(endpoint, ...args) {
      const options = /** @type {any} */ (args[0] || {});
      const [method, pathTemplate] = endpoint.split(" ");
      let path = pathTemplate;
      if (options.params) {
        const keys = Object.keys(options.params).sort(
          (a, b) => b.length - a.length,
        );
        for (const k of keys) {
          const v = options.params[k];
          if (v !== undefined && v !== null) {
            path = path.replace(":" + k, encodeURIComponent(String(v)));
            path = path.replace("*" + k, String(v));
          }
        }
      }
      if (/[:*][a-zA-Z0-9_]+/.test(path)) {
        throw new Error(
          "Missing required path parameter in endpoint URL: " +
            pathTemplate +
            " (resolved: " +
            path +
            ")",
        );
      }
      const cleanBase = baseUrl ? baseUrl.replace(/\/+$/, "") : "";
      const cleanPath = path.startsWith("/") ? path : "/" + path;
      let url = cleanBase + cleanPath;
      if (options.query) {
        const q = new URLSearchParams();
        for (const [k, v] of Object.entries(options.query)) {
          if (v !== undefined && v !== null) {
            q.set(k, String(v));
          }
        }
        const qs = q.toString();
        if (qs) url += (url.includes("?") ? "&" : "?") + qs;
      }
      const baseHeaders =
        typeof clientOptions.headers === "function"
          ? await clientOptions.headers()
          : clientOptions.headers;
      const headers = new Headers(baseHeaders);
      if (options.headers) {
        for (const [k, v] of Object.entries(options.headers)) {
          headers.set(k, String(v));
        }
      }
      let body = undefined;
      let duplex = undefined;
      if (options.body !== undefined) {
        if (
          typeof FormData !== "undefined" &&
          options.body instanceof FormData
        ) {
          body = options.body;
        } else if (
          typeof URLSearchParams !== "undefined" &&
          options.body instanceof URLSearchParams
        ) {
          body = options.body;
        } else if (
          typeof Blob !== "undefined" &&
          options.body instanceof Blob
        ) {
          body = options.body;
        } else if (
          typeof ArrayBuffer !== "undefined" &&
          (options.body instanceof ArrayBuffer ||
            ArrayBuffer.isView(options.body))
        ) {
          body = options.body;
        } else if (
          typeof ReadableStream !== "undefined" &&
          options.body instanceof ReadableStream
        ) {
          body = options.body;
          duplex = "half";
        } else {
          if (!headers.has("Content-Type")) {
            headers.set("Content-Type", "application/json");
          }
          body = JSON.stringify(options.body);
        }
      }
      const fetchFn = clientOptions.fetch || fetch;
      /** @type {any} */
      const fetchInit = {
        method,
        headers,
        body,
        credentials: options.credentials ?? clientOptions.credentials,
        signal: options.signal,
        cache: options.cache ?? clientOptions.cache,
      };
      const nextConfig = options.next ?? clientOptions.next;
      if (nextConfig !== undefined) {
        fetchInit.next = nextConfig;
      }
      if (duplex) {
        fetchInit.duplex = duplex;
      }
      const res = await fetchFn(url, fetchInit);
      if (!res.ok) {
        const err = await res.json().catch(() => ({ message: res.statusText }));
        throw new GotaHttpError(res.status, res.statusText, err, res.headers);
      }
      if (res.status === 204 || res.status === 205)
        return /** @type {any} */ (undefined);
      const ctype = res.headers.get("content-type") || "";
      if (ctype.includes("application/json") || ctype.includes("+json")) {
        return res.json();
      }
      if (ctype.includes("text/event-stream") && res.body) {
        return /** @type {any} */ (
          typeof TextDecoderStream !== "undefined"
            ? res.body.pipeThrough(new TextDecoderStream())
            : res.body
        );
      }
      const isBinary =
        /^(image|audio|video)\/|application\/(octet-stream|pdf|zip|gzip|vnd\.|x-)/.test(
          ctype,
        );
      if (isBinary && typeof res.blob === "function") {
        return res.blob();
      }
      if (ctype.startsWith("text/") || ctype.includes("xml")) {
        return res.text();
      }
      if (typeof res.blob === "function") {
        return res.blob();
      }
      return /** @type {any} */ (res.text());
    },
  };
}

// @ts-ignore - Vue is loaded globally via CDN
const { createApp, ref, reactive, computed, onMounted } = Vue;

createApp({
  setup() {
    /** @type {{ value: string }} */
    const baseUrl = ref("http://localhost:3000");
    const docsUrl = computed(
      () => (baseUrl.value ? baseUrl.value.replace(/\/+$/, "") : "") + "/docs",
    );
    /** @type {{ value: TodoResponse[] }} */
    const todos = ref([]);
    const loading = ref(false);
    const creating = ref(false);
    const connectionError = ref(false);
    /** @type {{ value: { message: string; fields?: Record<string, string> | null } | null }} */
    const createError = ref(null);
    /** @type {{ value: string | null }} */
    const lookupError = ref(null);
    /** @type {{ value: string }} */
    const searchId = ref("todo_1");
    /** @type {{ value: TodoResponse | null }} */
    const selectedTodo = ref(null);

    /**
     * Normalizes validation error arrays into clean comma-separated strings for UI display
     * @param {Record<string, string[] | string>} errors
     * @returns {Record<string, string>}
     */
    function formatErrors(errors) {
      /** @type {Record<string, string>} */
      const result = {};
      for (const [key, val] of Object.entries(errors)) {
        result[key] = Array.isArray(val) ? val.join(", ") : String(val);
      }
      return result;
    }

    /** @type {CreateTodoRequest} */
    const form = reactive({
      title: "",
      priority: "normal",
    });

    const client = () => createGotaClient(baseUrl.value);

    async function loadTodos() {
      loading.value = true;
      connectionError.value = false;
      try {
        todos.value = await client().fetch("GET /todos");
      } catch {
        connectionError.value = true;
      } finally {
        loading.value = false;
      }
    }

    async function createTodo() {
      creating.value = true;
      createError.value = null;
      try {
        const created = await client().fetch("POST /todos", {
          body: {
            title: form.title,
            priority: form.priority,
          },
        });
        todos.value.push(created);
        selectedTodo.value = created;
        searchId.value = created.id;
        form.title = "";
        form.priority = "normal";
      } catch (/** @type {any} */ err) {
        if (err instanceof GotaHttpError) {
          /** @type {PostTodoError} */
          const data = err.data;
          createError.value = {
            message: err.message,
            fields:
              data && "errors" in data && data.errors
                ? formatErrors(data.errors)
                : null,
          };
        } else {
          createError.value = {
            message: err?.message || String(err),
            fields: null,
          };
        }
      } finally {
        creating.value = false;
      }
    }

    /**
     * @param {string} id
     */
    async function fetchTodoById(id) {
      if (!id) return;
      lookupError.value = null;
      selectedTodo.value = null;
      try {
        selectedTodo.value = await client().fetch("GET /todos/:id", {
          params: { id: id.trim() },
        });
      } catch (/** @type {any} */ err) {
        if (err instanceof GotaHttpError) {
          lookupError.value = `${err.status}: ${err.message}`;
        } else {
          lookupError.value = err?.message || String(err);
        }
      }
    }

    /**
     * @param {string} id
     */
    function selectFromList(id) {
      searchId.value = id;
      fetchTodoById(id);
    }

    /**
     * @param {string} priority
     */
    function priorityBadgeClass(priority) {
      switch (priority) {
        case "high":
          return "bg-rose-500/10 text-rose-600 dark:text-rose-400 border-rose-500/25";
        case "low":
          return "bg-slate-500/10 text-slate-600 dark:text-slate-400 border-slate-500/25";
        default:
          return "bg-sky-500/10 text-sky-600 dark:text-sky-400 border-sky-500/25";
      }
    }

    onMounted(loadTodos);

    return {
      baseUrl,
      docsUrl,
      todos,
      loading,
      creating,
      connectionError,
      createError,
      lookupError,
      searchId,
      selectedTodo,
      form,
      loadTodos,
      createTodo,
      fetchTodoById,
      selectFromList,
      priorityBadgeClass,
    };
  },
}).mount("#app");
