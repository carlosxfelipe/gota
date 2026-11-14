// @ts-check

/**
 * @typedef {import("../codegen/dist/sdk").TodoResponse} TodoResponse
 * @typedef {import("../codegen/dist/sdk").CreateTodoRequest} CreateTodoRequest
 * @typedef {import("../codegen/dist/sdk").GotaClient} GotaClient
 * @typedef {import("../codegen/dist/sdk").GotaClientOptions} GotaClientOptions
 * @typedef {import("../codegen/dist/sdk").BaseRequestOptions} BaseRequestOptions
 * @typedef {import("../codegen/dist/sdk").BodyEncoding} BodyEncoding
 * @typedef {import("../codegen/dist/sdk").GotaRequestInit} GotaRequestInit
 * @typedef {import("../codegen/dist/sdk").GotaRequestConfig} GotaRequestConfig
 * @typedef {import("../codegen/dist/sdk").GotaResponseContext} GotaResponseContext
 * @typedef {import("../codegen/dist/sdk").ResponseError<"POST /todos">} PostTodoError
 */

const DEFAULT_RETRY_STATUSES = [408, 429, 500, 502, 503, 504];
const DEFAULT_RETRY_METHODS = ["GET", "HEAD", "OPTIONS", "PUT", "DELETE"];

/** @type {Record<string, BodyEncoding | undefined>} */
const BODY_ENCODINGS = {};

/**
 * @template [T=unknown]
 */
class GotaHttpError extends Error {
  /**
   * @param {number} status
   * @param {string} statusText
   * @param {any} data
   * @param {Headers} [headers]
   * @param {Response} [response]
   */
  constructor(status, statusText, data, headers, response) {
    super(errorMessage(data, statusText || "HTTP Error " + status));
    this.name = "GotaHttpError";
    this.status = status;
    this.statusText = statusText;
    this.data = data;
    this.headers = headers ?? new Headers();
    this.response = response;
    Object.setPrototypeOf(this, GotaHttpError.prototype);
  }
}

/**
 * @template [T=unknown]
 * @param {unknown} error
 * @returns {error is GotaHttpError<T>}
 */
function isGotaHttpError(error) {
  return error instanceof GotaHttpError;
}

/**
 * @param {unknown[]} items
 * @returns {string}
 */
function formatValidationErrors(items) {
  const parts = items.map((item) => {
    if (typeof item === "string") return item;
    if (item && typeof item === "object") {
      const e = /** @type {Record<string, unknown>} */ (item);
      const loc = Array.isArray(e["loc"])
        ? e["loc"].map(String).join(".")
        : typeof e["field"] === "string"
          ? e["field"]
          : typeof e["path"] === "string"
            ? e["path"]
            : "";
      const msg =
        typeof e["msg"] === "string"
          ? e["msg"]
          : typeof e["message"] === "string"
            ? e["message"]
            : typeof e["error"] === "string"
              ? e["error"]
              : "";
      if (loc && msg) return loc + ": " + msg;
      if (msg) return msg;
    }
    return JSON.stringify(item);
  });
  return "Validation error: " + parts.join("; ");
}

/**
 * @param {unknown} data
 * @param {string} fallback
 * @returns {string}
 */
function errorMessage(data, fallback) {
  if (typeof data === "string" && data.trim()) return data.trim();
  if (data && typeof data === "object") {
    const d = /** @type {Record<string, unknown>} */ (data);
    if (typeof d["message"] === "string" && d["message"]) return d["message"];
    if (typeof d["error"] === "string" && d["error"]) return d["error"];
    if (typeof d["detail"] === "string" && d["detail"]) return d["detail"];
    if (Array.isArray(d["detail"]) && d["detail"].length > 0)
      return formatValidationErrors(d["detail"]);
    if (Array.isArray(d["errors"]) && d["errors"].length > 0)
      return formatValidationErrors(d["errors"]);
    if (typeof d["title"] === "string" && d["title"]) return d["title"];
  }
  return fallback;
}

/**
 * @param {unknown} value
 * @returns {value is Blob}
 */
function isBlob(value) {
  return (
    (typeof Blob !== "undefined" && value instanceof Blob) ||
    (typeof File !== "undefined" && value instanceof File)
  );
}

/**
 * @param {unknown} value
 * @returns {value is ReadableStream}
 */
function isReadableStream(value) {
  return (
    (typeof ReadableStream !== "undefined" &&
      value instanceof ReadableStream) ||
    (value !== null &&
      typeof value === "object" &&
      typeof (/** @type {{ getReader?: unknown }} */ (value).getReader) ===
        "function")
  );
}

/**
 * @param {unknown} body
 * @returns {body is BodyInit}
 */
function isBodyInit(body) {
  return (
    (typeof FormData !== "undefined" && body instanceof FormData) ||
    (typeof URLSearchParams !== "undefined" &&
      body instanceof URLSearchParams) ||
    isBlob(body) ||
    isReadableStream(body) ||
    (typeof ArrayBuffer !== "undefined" &&
      (body instanceof ArrayBuffer || ArrayBuffer.isView(body)))
  );
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
 * @param {string} key
 * @param {unknown} value
 * @param {Array<[string, string | Blob]>} out
 */
function collectEntries(key, value, out) {
  if (value === undefined || value === null) return;
  if (Array.isArray(value)) {
    for (const item of value) collectEntries(key, item, out);
  } else if (isBlob(value)) {
    out.push([key, value]);
  } else if (value instanceof Date) {
    out.push([key, value.toISOString()]);
  } else if (typeof value === "object") {
    out.push([key, JSON.stringify(value)]);
  } else {
    out.push([key, String(value)]);
  }
}

/**
 * @param {object} source
 * @returns {Array<[string, string | Blob]>}
 */
function toEntries(source) {
  /** @type {Array<[string, string | Blob]>} */
  const out = [];
  for (const [key, value] of Object.entries(source))
    collectEntries(key, value, out);
  return out;
}

/**
 * @param {unknown} value
 * @param {Set<unknown>} [seen]
 * @returns {boolean}
 */
function hasBlobValue(value, seen = new Set()) {
  if (value === null || typeof value !== "object") return false;
  if (isBlob(value)) return true;
  if (seen.has(value)) return false;
  seen.add(value);
  if (Array.isArray(value))
    return value.some((item) => hasBlobValue(item, seen));
  if (value instanceof Date) return false;
  return Object.values(value).some((item) => hasBlobValue(item, seen));
}

/**
 * @param {string} endpoint
 * @param {string} template
 * @param {Record<string, unknown>} params
 * @returns {string}
 */
function resolvePath(endpoint, template, params) {
  return template
    .split("/")
    .map((segment) => {
      const kind = segment.charAt(0);
      if (kind !== ":" && kind !== "*") return segment;
      const name = segment.slice(1) || "wildcard";
      const value = params[name];
      if (value === undefined || value === null) {
        throw new Error(
          'Missing required path parameter "' + name + '" for ' + endpoint,
        );
      }
      if (kind === "*") {
        const parts = Array.isArray(value)
          ? value.map(String)
          : String(value).replace(/^\/+/, "").split("/");
        return parts.map(encodeURIComponent).join("/");
      }
      return encodeURIComponent(String(value));
    })
    .join("/");
}

/**
 * @param {string} endpoint
 * @param {unknown} body
 * @param {Headers} headers
 * @returns {BodyInit}
 */
function encodeBody(endpoint, body, headers) {
  if (typeof FormData !== "undefined" && body instanceof FormData) {
    headers.delete("Content-Type");
    return body;
  }
  if (isBodyInit(body)) return body;
  const encoding = BODY_ENCODINGS[endpoint] ?? "json";
  const contentType = headers.get("Content-Type") ?? "";
  if (body !== null && typeof body === "object") {
    if (
      encoding === "multipart" ||
      contentType.includes("multipart/form-data") ||
      hasBlobValue(body)
    ) {
      const form = new FormData();
      for (const [key, value] of toEntries(body)) form.append(key, value);
      headers.delete("Content-Type");
      return form;
    }
    if (
      encoding === "form" ||
      contentType.includes("application/x-www-form-urlencoded")
    ) {
      const form = new URLSearchParams();
      for (const [key, value] of toEntries(body)) {
        if (typeof value === "string") form.append(key, value);
      }
      return form;
    }
  }
  if (
    typeof body === "string" &&
    (encoding === "raw" ||
      (contentType !== "" && !contentType.includes("json")))
  ) {
    return body;
  }
  if (!contentType) headers.set("Content-Type", "application/json");
  return JSON.stringify(body);
}

/**
 * @param {number | undefined} timeoutMs
 * @returns {AbortSignal | undefined}
 */
function createTimeoutSignal(timeoutMs) {
  if (timeoutMs === undefined || timeoutMs <= 0) return undefined;
  if (
    typeof AbortSignal !== "undefined" &&
    typeof AbortSignal.timeout === "function"
  )
    return AbortSignal.timeout(timeoutMs);
  if (typeof AbortController === "undefined") return undefined;
  const controller = new AbortController();
  const timeoutReason =
    typeof DOMException !== "undefined"
      ? new DOMException("The operation timed out.", "TimeoutError")
      : new Error("The operation timed out.");
  const timer = setTimeout(() => controller.abort(timeoutReason), timeoutMs);
  if (
    timer &&
    typeof timer === "object" &&
    "unref" in timer &&
    typeof (/** @type {{ unref?: unknown }} */ (timer).unref) === "function"
  ) {
    /** @type {{ unref: () => void }} */ (timer).unref();
  }
  return controller.signal;
}

/**
 * @param {AbortSignal | undefined} a
 * @param {AbortSignal | undefined} b
 * @returns {AbortSignal | undefined}
 */
function combineSignals(a, b) {
  if (!a) return b;
  if (!b) return a;
  const any = /** @type {any} */ (AbortSignal).any;
  if (typeof any === "function") return any.call(AbortSignal, [a, b]);
  const controller = new AbortController();
  const cleanup = () => {
    a.removeEventListener("abort", onAbortA);
    b.removeEventListener("abort", onAbortB);
  };
  const onAbortA = () => {
    cleanup();
    controller.abort(a.reason);
  };
  const onAbortB = () => {
    cleanup();
    controller.abort(b.reason);
  };
  if (a.aborted) controller.abort(a.reason);
  else if (b.aborted) controller.abort(b.reason);
  else {
    a.addEventListener("abort", onAbortA, { once: true });
    b.addEventListener("abort", onAbortB, { once: true });
  }
  return controller.signal;
}

/**
 * @param {number} baseDelay
 * @param {number} attempt
 * @param {Response} [response]
 * @returns {number}
 */
function retryDelay(baseDelay, attempt, response) {
  let delay =
    baseDelay * Math.pow(2, attempt - 1) * (0.5 + Math.random() * 0.5);
  const retryAfter = response?.headers?.get("Retry-After");
  if (retryAfter) {
    const seconds = parseInt(retryAfter, 10);
    if (!isNaN(seconds)) {
      delay = seconds * 1000;
    } else {
      const dateMs = Date.parse(retryAfter);
      if (!isNaN(dateMs)) delay = Math.max(0, dateMs - Date.now());
    }
  }
  return delay;
}

/**
 * @param {number} ms
 * @returns {Promise<void>}
 */
function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * @param {Response} res
 * @returns {Promise<unknown>}
 */
async function readErrorBody(res) {
  if (typeof res.text === "function") {
    const text = await res.text().catch(() => "");
    try {
      return text ? JSON.parse(text) : { message: res.statusText };
    } catch {
      return { message: text || res.statusText };
    }
  }
  if (typeof res.json === "function") {
    return res.json().catch(() => ({ message: res.statusText }));
  }
  return { message: res.statusText };
}

/**
 * @param {Response} res
 * @returns {Promise<any>}
 */
async function readBody(res) {
  if (
    res.status === 204 ||
    res.status === 205 ||
    res.headers.get("content-length") === "0"
  )
    return undefined;
  const contentType = res.headers.get("content-type") ?? "";
  if (
    contentType.includes("application/json") ||
    contentType.includes("+json")
  ) {
    if (typeof res.text === "function") {
      const text = await res.text();
      return text ? JSON.parse(text) : undefined;
    }
    return res.json();
  }
  if (contentType.includes("text/event-stream") && res.body) {
    return typeof TextDecoderStream !== "undefined"
      ? res.body.pipeThrough(new TextDecoderStream())
      : res.body;
  }
  const isBinary =
    /^(image|audio|video)\/|application\/(octet-stream|pdf|zip|gzip|vnd\.|x-)/.test(
      contentType,
    );
  if (isBinary && typeof res.blob === "function") return res.blob();
  if (contentType.startsWith("text/") || contentType.includes("xml"))
    return res.text();
  if (typeof res.blob === "function") return res.blob();
  return res.text();
}

/**
 * Exact runtime implementation of createGotaClient() generated in examples/codegen/dist/sdk.ts
 * @param {string} [baseUrl=""]
 * @param {GotaClientOptions} [clientOptions={}]
 * @returns {GotaClient}
 */
function createGotaClient(baseUrl = "", clientOptions = {}) {
  const root = baseUrl.replace(/\/+$/, "");

  /**
   * @param {string} endpoint
   * @param {any} options
   * @returns {Promise<any>}
   */
  async function request(endpoint, options) {
    const separator = endpoint.indexOf(" ");
    const method = endpoint.slice(0, separator);
    const template = endpoint.slice(separator + 1);
    const path = resolvePath(endpoint, template, options.params ?? {});

    let url = root + (path.startsWith("/") ? path : "/" + path);
    if (options.query) {
      const search = new URLSearchParams();
      for (const [key, value] of Object.entries(options.query))
        appendQueryParam(search, key, value);
      const qs = search.toString();
      if (qs) url += (url.includes("?") ? "&" : "?") + qs;
    }

    const baseHeaders =
      typeof clientOptions.headers === "function"
        ? await clientOptions.headers()
        : clientOptions.headers;
    const headers = new Headers(baseHeaders);
    if (options.headers) {
      for (const [key, value] of Object.entries(options.headers))
        headers.set(key, String(value));
    }

    /** @type {GotaRequestInit} */
    const init = { method, headers };
    if (options.body !== undefined) {
      init.body = encodeBody(endpoint, options.body, headers);
    }
    const credentials = options.credentials ?? clientOptions.credentials;
    if (credentials) init.credentials = credentials;
    const cache = options.cache ?? clientOptions.cache;
    if (cache) init.cache = cache;
    const nextConfig =
      clientOptions.next || options.next
        ? { ...clientOptions.next, ...options.next }
        : undefined;
    if (nextConfig) init.next = nextConfig;
    const duplex =
      options.duplex ?? (isReadableStream(init.body) ? "half" : undefined);
    if (duplex) init.duplex = duplex;

    const signal = combineSignals(
      options.signal,
      createTimeoutSignal(options.timeoutMs ?? clientOptions.timeoutMs),
    );
    if (signal) init.signal = signal;

    if (clientOptions.onRequest)
      await clientOptions.onRequest({ endpoint, url, init });
    if (options.onRequest) await options.onRequest({ endpoint, url, init });

    const retryOpt = options.retry ?? clientOptions.retry;
    let maxAttempts = 1;
    let baseDelay = 300;
    let retryStatuses = DEFAULT_RETRY_STATUSES;
    let retryMethods = DEFAULT_RETRY_METHODS;
    if (typeof retryOpt === "number" && retryOpt > 0) {
      maxAttempts = 1 + retryOpt;
    } else if (typeof retryOpt === "object" && retryOpt !== null) {
      if (typeof retryOpt.attempts === "number" && retryOpt.attempts > 0)
        maxAttempts = 1 + retryOpt.attempts;
      if (typeof retryOpt.delayMs === "number" && retryOpt.delayMs >= 0)
        baseDelay = retryOpt.delayMs;
      if (Array.isArray(retryOpt.statusCodes))
        retryStatuses = retryOpt.statusCodes;
      if (Array.isArray(retryOpt.methods))
        retryMethods = retryOpt.methods.map((/** @type {string} */ m) =>
          m.toUpperCase(),
        );
    }
    if (!retryMethods.includes(method.toUpperCase())) maxAttempts = 1;

    const fetchFn = clientOptions.fetch ?? globalThis.fetch.bind(globalThis);
    /** @type {Response} */
    let res;
    let attempt = 0;
    while (true) {
      try {
        res = await fetchFn(url, init);
      } catch (fetchErr) {
        if (attempt < maxAttempts - 1 && !init.signal?.aborted) {
          attempt++;
          await sleep(retryDelay(baseDelay, attempt));
          continue;
        }
        const errObj =
          fetchErr instanceof Error ? fetchErr : new Error(String(fetchErr));
        if (clientOptions.onError)
          await clientOptions.onError(errObj, { endpoint, url });
        if (options.onError) await options.onError(errObj, { endpoint, url });
        throw fetchErr;
      }
      if (
        !res.ok &&
        attempt < maxAttempts - 1 &&
        retryStatuses.includes(res.status) &&
        !init.signal?.aborted
      ) {
        attempt++;
        await sleep(retryDelay(baseDelay, attempt, res));
        continue;
      }
      break;
    }

    /** @type {GotaResponseContext} */
    const context = { endpoint, url };
    if (clientOptions.onResponse) await clientOptions.onResponse(res, context);
    if (options.onResponse) await options.onResponse(res, context);

    if (!res.ok) {
      const data = await readErrorBody(res);
      const httpErr = new GotaHttpError(
        res.status,
        res.statusText,
        data,
        res.headers,
        res,
      );
      if (clientOptions.onError) await clientOptions.onError(httpErr, context);
      if (options.onError) await options.onError(httpErr, context);
      throw httpErr;
    }
    return readBody(res);
  }

  return {
    baseUrl: root,
    fetch(endpoint, ...args) {
      const [options] = /** @type {[any?]} */ (args);
      return request(String(endpoint), options ?? {});
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
        if (isGotaHttpError(err)) {
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
        if (isGotaHttpError(err)) {
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
