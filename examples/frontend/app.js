// @ts-check

/**
 * @typedef {import("../codegen/dist/sdk").TodoResponse} TodoResponse
 * @typedef {import("../codegen/dist/sdk").CreateTodoRequest} CreateTodoRequest
 * @typedef {import("../codegen/dist/sdk").GotaClient} GotaClient
 * @typedef {import("../codegen/dist/sdk").ResponseError<"POST /todos">} PostTodoError
 */

/** @type {Set<string>} */
const MULTIPART_ROUTES = new Set();
/** @type {Set<string>} */
const FORM_ROUTES = new Set();

class GotaHttpError extends Error {
  /**
   * @param {number} status
   * @param {string} statusText
   * @param {any} data
   * @param {Headers} [headers]
   */
  constructor(status, statusText, data, headers) {
    let message = statusText || "HTTP Error " + status;
    if (data && typeof data === "object") {
      if (typeof data.message === "string" && data.message)
        message = data.message;
      else if (typeof data.error === "string" && data.error)
        message = data.error;
      else if (typeof data.detail === "string" && data.detail)
        message = data.detail;
      else if (Array.isArray(data.detail) && data.detail.length > 0)
        message = "Validation error: " + JSON.stringify(data.detail);
      else if (Array.isArray(data.errors) && data.errors.length > 0)
        message = "Validation error: " + JSON.stringify(data.errors);
      else if (typeof data.title === "string" && data.title)
        message = data.title;
    } else if (typeof data === "string" && data.trim()) {
      message = data.trim();
    }
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
 * Appends a query parameter value to URLSearchParams.
 * @param {URLSearchParams} search
 * @param {string} key
 * @param {unknown} value
 */
function appendQueryParam(search, key, value) {
  if (value === undefined || value === null) return;
  if (Array.isArray(value)) {
    for (const item of value) appendQueryParam(search, key, item);
  } else if (value instanceof Date) {
    search.append(key, value.toISOString());
  } else if (typeof value === "object") {
    search.append(key, JSON.stringify(value));
  } else {
    search.append(key, String(value));
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
            path = path.replace(
              "*" + k,
              String(v).split("/").map(encodeURIComponent).join("/"),
            );
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
          appendQueryParam(q, k, v);
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
        } else if (options.body !== null && typeof options.body === "object") {
          const isPlain =
            /** @type {any} */ (options.body).constructor === Object ||
            !(/** @type {any} */ (options.body).constructor);
          const hasBlob =
            isPlain &&
            Object.values(options.body).some(
              (/** @type {any} */ val) =>
                (typeof Blob !== "undefined" && val instanceof Blob) ||
                (Array.isArray(val) &&
                  val.some(
                    (/** @type {any} */ item) =>
                      typeof Blob !== "undefined" && item instanceof Blob,
                  )),
            );
          const ctypeHeader = headers.get("Content-Type") || "";
          const isMultipart =
            MULTIPART_ROUTES.has(String(endpoint)) ||
            hasBlob ||
            ctypeHeader.includes("multipart/form-data");
          const isFormUrl =
            FORM_ROUTES.has(String(endpoint)) ||
            ctypeHeader.includes("application/x-www-form-urlencoded");

          if (isMultipart && typeof FormData !== "undefined") {
            const form = new FormData();
            for (const [k, v] of Object.entries(options.body)) {
              if (v === undefined || v === null) continue;
              if (Array.isArray(v)) {
                for (const item of v) {
                  if (item !== undefined && item !== null)
                    form.append(k, /** @type {any} */ (item));
                }
              } else {
                form.append(k, /** @type {any} */ (v));
              }
            }
            body = form;
            headers.delete("Content-Type");
          } else if (isFormUrl && typeof URLSearchParams !== "undefined") {
            const form = new URLSearchParams();
            for (const [k, v] of Object.entries(options.body)) {
              if (v === undefined || v === null) continue;
              if (Array.isArray(v)) {
                for (const item of v) {
                  if (item !== undefined && item !== null)
                    form.append(k, String(item));
                }
              } else {
                form.set(k, String(v));
              }
            }
            body = form;
          } else {
            if (!headers.has("Content-Type")) {
              headers.set("Content-Type", "application/json");
            }
            body = JSON.stringify(options.body);
          }
        } else {
          body = options.body;
        }
      }
      const timeout = options.timeoutMs ?? clientOptions.timeoutMs;
      /** @type {AbortSignal | undefined} */
      let signal = options.signal;
      if (timeout && typeof AbortSignal !== "undefined") {
        const timeoutSignal =
          typeof AbortSignal.timeout === "function"
            ? AbortSignal.timeout(timeout)
            : (() => {
                const controller = new AbortController();
                setTimeout(
                  () =>
                    controller.abort(
                      new DOMException(
                        "The operation timed out.",
                        "TimeoutError",
                      ),
                    ),
                  timeout,
                );
                return controller.signal;
              })();
        if (signal) {
          if (typeof (/** @type {any} */ (AbortSignal).any) === "function") {
            signal = /** @type {any} */ (AbortSignal).any([
              signal,
              timeoutSignal,
            ]);
          } else {
            const controller = new AbortController();
            signal.addEventListener("abort", () =>
              controller.abort(signal?.reason),
            );
            timeoutSignal.addEventListener("abort", () =>
              controller.abort(timeoutSignal.reason),
            );
            signal = controller.signal;
          }
        } else {
          signal = timeoutSignal;
        }
      }
      const fetchFn = clientOptions.fetch || fetch;
      /** @type {any} */
      const fetchInit = {
        method,
        headers,
        body,
        credentials: options.credentials ?? clientOptions.credentials,
        signal,
        cache: options.cache ?? clientOptions.cache,
      };
      const nextConfig = options.next ?? clientOptions.next;
      if (nextConfig !== undefined) {
        fetchInit.next = nextConfig;
      }
      if (duplex) {
        fetchInit.duplex = duplex;
      }

      const reqConfig = { endpoint, url, init: fetchInit };
      const onRequest = options.onRequest ?? clientOptions.onRequest;
      if (onRequest) {
        await onRequest(reqConfig);
      }

      const retryOpt = options.retry ?? clientOptions.retry;
      let maxAttempts = 1;
      let baseDelay = 300;
      let retryStatuses = [408, 429, 500, 502, 503, 504];
      if (typeof retryOpt === "number" && retryOpt > 0) {
        maxAttempts = 1 + retryOpt;
      } else if (typeof retryOpt === "object" && retryOpt !== null) {
        if (typeof retryOpt.attempts === "number" && retryOpt.attempts > 0) {
          maxAttempts = 1 + retryOpt.attempts;
        }
        if (typeof retryOpt.delayMs === "number" && retryOpt.delayMs >= 0) {
          baseDelay = retryOpt.delayMs;
        }
        if (Array.isArray(retryOpt.statusCodes)) {
          retryStatuses = retryOpt.statusCodes;
        }
      }

      let lastRes = undefined;
      let attempt = 0;
      while (true) {
        try {
          const res = await fetchFn(url, fetchInit);
          lastRes = res;
          if (
            !res.ok &&
            attempt < maxAttempts - 1 &&
            retryStatuses.includes(res.status)
          ) {
            attempt++;
            let delay =
              baseDelay *
              Math.pow(2, attempt - 1) *
              (0.5 + Math.random() * 0.5);
            const retryAfter = res.headers
              ? res.headers.get("Retry-After")
              : null;
            if (retryAfter) {
              const seconds = parseInt(retryAfter, 10);
              if (!isNaN(seconds)) {
                delay = seconds * 1000;
              } else {
                const dateMs = Date.parse(retryAfter);
                if (!isNaN(dateMs)) delay = Math.max(0, dateMs - Date.now());
              }
            }
            await new Promise((resolve) => setTimeout(resolve, delay));
            continue;
          }
          break;
        } catch (fetchErr) {
          if (attempt < maxAttempts - 1 && (!signal || !signal.aborted)) {
            attempt++;
            const delay =
              baseDelay *
              Math.pow(2, attempt - 1) *
              (0.5 + Math.random() * 0.5);
            await new Promise((resolve) => setTimeout(resolve, delay));
            continue;
          }
          throw fetchErr;
        }
      }
      const res = lastRes;

      const onResponse = options.onResponse ?? clientOptions.onResponse;
      if (onResponse) {
        await onResponse(res, { endpoint, url });
      }

      if (!res.ok) {
        let err;
        if (typeof res.text === "function") {
          const text = await res.text().catch(() => "");
          try {
            err = text ? JSON.parse(text) : { message: res.statusText };
          } catch {
            err = text ? { message: text } : { message: res.statusText };
          }
        } else if (typeof res.json === "function") {
          err = await res.json().catch(() => ({ message: res.statusText }));
        } else {
          err = { message: res.statusText };
        }
        const httpErr = new GotaHttpError(
          res.status,
          res.statusText,
          err,
          res.headers,
        );
        const onError = options.onError ?? clientOptions.onError;
        if (onError) {
          await onError(httpErr, { endpoint, url });
        }
        throw httpErr;
      }
      if (
        res.status === 204 ||
        res.status === 205 ||
        res.headers.get("content-length") === "0"
      )
        return /** @type {any} */ (undefined);
      const ctype = res.headers.get("content-type") || "";
      if (ctype.includes("application/json") || ctype.includes("+json")) {
        if (typeof res.text === "function") {
          const text = await res.text();
          return text ? JSON.parse(text) : undefined;
        }
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

    const client = () =>
      createGotaClient(baseUrl.value, {
        timeoutMs: 8000,
        retry: { attempts: 2, delayMs: 250 },
      });

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
