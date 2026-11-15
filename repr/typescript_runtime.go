package repr

// TypeScript runtime templates emitted into the generated declarations (.d.ts) and SDK (.ts).
//
// Constraints for the TypeScript below, so the output works everywhere:
//   - zero dependencies: only fetch, Headers, URLSearchParams, FormData, AbortController;
//   - "erasable" syntax only (no enums, namespaces or parameter properties), so the SDK runs
//     directly with `node sdk.ts` (type stripping), Deno, Bun and any bundler;
//   - compiles under `strict`, `exactOptionalPropertyTypes` and `noUncheckedIndexedAccess`;
//   - no template literals (backticks), since the templates live in Go raw strings;
//   - no top-level side effects, so bundlers can tree-shake unused helpers.
//
// The runtime is split in two layers:
//   - the generic core (tsRuntimeTypes + tsRuntimeImplementation / tsRuntimeDeclarations) knows
//     nothing about the API routes. In multi-file mode it is emitted verbatim as runtime.ts, so
//     the file only changes when Gota itself changes;
//   - the bindings (tsBindingTypes + tsBindingImplementation / tsBindingDeclarations) specialize
//     the core for the generated GotaEndpoints map and BODY_ENCODINGS table.

// tsRuntimeExportedNames lists every name exported (or declared at top level) by the runtime
// and the bindings. Generated schema types must never shadow them.
var tsRuntimeExportedNames = []string{
	// Bindings.
	"GotaEndpoints",
	"EndpointKey",
	"RequestParams",
	"RequestQuery",
	"RequestBody",
	"ResponseData",
	"ResponseError",
	"RequestOptions",
	"GotaClient",
	"createGotaClient",
	"isEndpointError",
	"BODY_ENCODINGS",
	// Generic runtime core.
	"NextFetchRequestConfig",
	"GotaRetryConfig",
	"GotaRequestInit",
	"GotaRequestConfig",
	"GotaResponseContext",
	"BaseRequestOptions",
	"BodyEncoding",
	"BodyEncodingMap",
	"GotaClientOptions",
	"EndpointDefinition",
	"EndpointMap",
	"RequestOptionsFor",
	"RequestArgs",
	"GotaQueryKeyArgs",
	"GotaQueryOptions",
	"GotaClientFor",
	"GotaErrorKind",
	"GotaErrorDetails",
	"GotaHttpError",
	"isGotaHttpError",
	"isAbortError",
	"parseGotaError",
	"DEFAULT_RETRY_METHODS",
	"createGotaClientFor",
	// Internal (non-exported) runtime identifiers.
	"RawRequestOptions",
	"FormEntry",
	"NETWORK_ERRORS",
	"GOTA_ERROR_BRAND",
}

// tsBindingTypes specializes the generic runtime types for the generated GotaEndpoints map.
const tsBindingTypes = `export type EndpointKey = keyof GotaEndpoints;

export type RequestParams<K extends EndpointKey> = GotaEndpoints[K]["params"];
export type RequestQuery<K extends EndpointKey> = GotaEndpoints[K]["query"];
export type RequestBody<K extends EndpointKey> = GotaEndpoints[K]["body"];
export type ResponseData<K extends EndpointKey> = GotaEndpoints[K]["response"];
export type ResponseError<K extends EndpointKey> = GotaEndpoints[K]["error"];

export type RequestOptions<K extends EndpointKey> = RequestOptionsFor<GotaEndpoints, K>;

export type GotaClient = GotaClientFor<GotaEndpoints>;
`

const tsBindingDeclarations = `
export declare function createGotaClient(baseUrl?: string, clientOptions?: GotaClientOptions): GotaClient;

export declare function isEndpointError<K extends EndpointKey>(
  error: unknown,
  endpoint: K,
): error is GotaHttpError<ResponseError<K>>;
`

const tsBindingImplementation = `
/** Creates a typed client for this API. */
export function createGotaClient(baseUrl = "", clientOptions: GotaClientOptions = {}): GotaClient {
  return createGotaClientFor<GotaEndpoints>(baseUrl, clientOptions, BODY_ENCODINGS);
}

/** Narrows an error to the documented error payload of a specific endpoint. */
export function isEndpointError<K extends EndpointKey>(
  error: unknown,
  endpoint: K,
): error is GotaHttpError<ResponseError<K>> {
  return isGotaHttpError(error) && error.endpoint === endpoint;
}
`

// tsRuntimeTypes holds the route-independent runtime types.
const tsRuntimeTypes = `export interface NextFetchRequestConfig {
  revalidate?: number | false;
  tags?: string[];
}

export interface GotaRetryConfig {
  /** Maximum number of retry attempts (in addition to the initial request). */
  attempts?: number;
  /** Base delay in milliseconds for the exponential backoff (default: 300). */
  delayMs?: number;
  /** HTTP status codes that trigger a retry (default: [408, 429, 500, 502, 503, 504]). */
  statusCodes?: number[];
  /** HTTP methods eligible for retries (default: idempotent methods GET, HEAD, OPTIONS, PUT, DELETE). */
  methods?: string[];
}

/** The RequestInit handed to fetch (headers are always a Headers instance). */
export interface GotaRequestInit extends RequestInit {
  headers: Headers;
  next?: NextFetchRequestConfig;
  duplex?: "half";
}

export interface GotaRequestConfig {
  endpoint: string;
  url: string;
  init: GotaRequestInit;
}

export interface GotaResponseContext {
  endpoint: string;
  url: string;
}

export interface BaseRequestOptions {
  headers?: Record<string, string>;
  credentials?: RequestCredentials;
  /**
   * Cancels the request (e.g. from an AbortController in a React effect cleanup).
   * Cancellations requested through this signal are not reported to onError hooks.
   */
  signal?: AbortSignal;
  cache?: RequestCache;
  next?: NextFetchRequestConfig;
  duplex?: "half";
  timeoutMs?: number;
  retry?: number | GotaRetryConfig;
  onRequest?: (config: GotaRequestConfig) => void | Promise<void>;
  onResponse?: (response: Response, config: GotaResponseContext) => void | Promise<void>;
  onError?: (error: GotaHttpError | Error, config: GotaResponseContext) => void | Promise<void>;
}

export type BodyEncoding = "json" | "form" | "multipart" | "raw";

/** Request body encodings of endpoints that do not send JSON, keyed by "METHOD /path". */
export type BodyEncodingMap = Readonly<Record<string, BodyEncoding | undefined>>;

export interface GotaClientOptions {
  /** Static headers, or a (sync/async) callback evaluated on every request (e.g. JWT). */
  headers?: Record<string, string> | (() => Record<string, string> | Promise<Record<string, string>>);
  credentials?: RequestCredentials;
  cache?: RequestCache;
  /** Default Next.js App Router cache configuration (revalidate / tags). */
  next?: NextFetchRequestConfig;
  /** Default request timeout in milliseconds. */
  timeoutMs?: number;
  /** Default retry policy (network errors or retryable status codes on idempotent methods). */
  retry?: number | GotaRetryConfig;
  onRequest?: (config: GotaRequestConfig) => void | Promise<void>;
  onResponse?: (response: Response, config: GotaResponseContext) => void | Promise<void>;
  onError?: (error: GotaHttpError | Error, config: GotaResponseContext) => void | Promise<void>;
  /** Custom fetch implementation (defaults to the global fetch). */
  fetch?: typeof fetch;
}

/** Shape of one entry of a generated endpoint map (such as GotaEndpoints). */
export interface EndpointDefinition {
  method: string;
  path: string;
  params: unknown;
  query: unknown;
  body: unknown;
  response: unknown;
  error: unknown;
  request: object;
}

/** Constraint satisfied by every generated endpoint map. */
export type EndpointMap<E> = { [K in keyof E]: EndpointDefinition };

export type RequestOptionsFor<E extends EndpointMap<E>, K extends keyof E> = E[K]["request"] & BaseRequestOptions;

/** Options argument of client methods: optional when the endpoint has no required params, query or body. */
export type RequestArgs<E extends EndpointMap<E>, K extends keyof E> =
  Record<never, never> extends E[K]["request"]
    ? [options?: RequestOptionsFor<E, K>]
    : [options: RequestOptionsFor<E, K>];

export interface GotaQueryKeyArgs {
  params?: unknown;
  query?: unknown;
  body?: unknown;
}

/**
 * Framework-agnostic query descriptor: pass it to TanStack Query (useQuery, queryOptions,
 * prefetchQuery) or use queryKey/queryFn with SWR. The query key always starts with the
 * endpoint, so invalidateQueries({ queryKey: ["GET /todos"] }) matches every variant.
 */
export interface GotaQueryOptions<K extends string, R> {
  queryKey: readonly [K, GotaQueryKeyArgs];
  queryFn: (context?: { signal?: AbortSignal | undefined }) => Promise<R>;
}

export interface GotaClientFor<E extends EndpointMap<E>> {
  readonly baseUrl: string;
  fetch<K extends keyof E & string>(endpoint: K, ...args: RequestArgs<E, K>): Promise<E[K]["response"]>;
  queryOptions<K extends keyof E & string>(
    endpoint: K,
    ...args: RequestArgs<E, K>
  ): GotaQueryOptions<K, E[K]["response"]>;
}

export type GotaErrorKind = "http" | "validation" | "timeout" | "abort" | "network" | "unknown";

/** Normalized error information returned by parseGotaError. */
export interface GotaErrorDetails {
  kind: GotaErrorKind;
  /** Human-readable message, safe to display. */
  message: string;
  /** HTTP status code (only for HTTP errors). */
  status?: number;
  /** Per-field validation messages (empty when the error is not a validation error). */
  fieldErrors: Record<string, string[]>;
  requestId?: string;
  /** Endpoint key ("METHOD /path") that produced the error. */
  endpoint?: string;
  /** Raw error payload returned by the API. */
  data?: unknown;
  /** The original thrown value. */
  cause: unknown;
}
`

const tsRuntimeDeclarations = `
export declare const DEFAULT_RETRY_METHODS: readonly string[];

export declare class GotaHttpError<T = unknown> extends Error {
  readonly status: number;
  readonly statusText: string;
  readonly data: T;
  readonly headers: Headers;
  readonly response: Response | undefined;
  readonly endpoint: string | undefined;
  constructor(status: number, statusText: string, data: T, headers?: Headers, response?: Response, endpoint?: string);
}

export declare function isGotaHttpError<T = unknown>(error: unknown): error is GotaHttpError<T>;

export declare function isAbortError(error: unknown): boolean;

export declare function parseGotaError(error: unknown): GotaErrorDetails;

export declare function createGotaClientFor<E extends EndpointMap<E>>(
  baseUrl?: string,
  clientOptions?: GotaClientOptions,
  bodyEncodings?: BodyEncodingMap,
): GotaClientFor<E>;
`

const tsRuntimeImplementation = `
const DEFAULT_RETRY_STATUSES = [408, 429, 500, 502, 503, 504];
export const DEFAULT_RETRY_METHODS: readonly string[] = ["GET", "HEAD", "OPTIONS", "PUT", "DELETE"];

/** Errors thrown by fetch itself (connection refused, DNS failure, CORS...). */
const NETWORK_ERRORS: WeakSet<object> = /* @__PURE__ */ new WeakSet<object>();
const GOTA_ERROR_BRAND = Symbol.for("gota.httpError");

export class GotaHttpError<T = unknown> extends Error {
  readonly status: number;
  readonly statusText: string;
  readonly data: T;
  readonly headers: Headers;
  readonly response: Response | undefined;
  readonly endpoint: string | undefined;

  constructor(status: number, statusText: string, data: T, headers?: Headers, response?: Response, endpoint?: string) {
    super(errorMessage(data, statusText || "HTTP Error " + status));
    this.name = "GotaHttpError";
    this.status = status;
    this.statusText = statusText;
    this.data = data;
    this.headers = headers ?? new Headers();
    this.response = response;
    this.endpoint = endpoint;
    Object.defineProperty(this, GOTA_ERROR_BRAND, { value: true });
    Object.setPrototypeOf(this, GotaHttpError.prototype);
  }
}

export function isGotaHttpError<T = unknown>(error: unknown): error is GotaHttpError<T> {
  return (
    error instanceof GotaHttpError ||
    (error !== null && typeof error === "object" && (error as Record<symbol, unknown>)[GOTA_ERROR_BRAND] === true)
  );
}

/** Reports whether the error comes from a cancelled request (AbortController.abort()). Timeouts return false. */
export function isAbortError(error: unknown): boolean {
  return errorName(error) === "AbortError";
}

/**
 * Normalizes anything thrown by the client into a display-friendly structure, including
 * per-field validation messages (Gota ValidationError) and the request ID (Gota ErrorResponse).
 */
export function parseGotaError(error: unknown): GotaErrorDetails {
  if (isGotaHttpError(error)) {
    const fieldErrors = extractFieldErrors(error.data);
    const details: GotaErrorDetails = {
      kind: Object.keys(fieldErrors).length > 0 ? "validation" : "http",
      message: error.message,
      status: error.status,
      fieldErrors,
      data: error.data,
      cause: error,
    };
    const requestId = readStringField(error.data, "requestId") || error.headers.get("X-Request-Id");
    if (requestId) details.requestId = requestId;
    if (error.endpoint) details.endpoint = error.endpoint;
    return details;
  }
  if (isAbortError(error)) {
    return { kind: "abort", message: "The request was cancelled.", fieldErrors: {}, cause: error };
  }
  if (errorName(error) === "TimeoutError") {
    return { kind: "timeout", message: "The request timed out.", fieldErrors: {}, cause: error };
  }
  if (error !== null && typeof error === "object" && NETWORK_ERRORS.has(error)) {
    return { kind: "network", message: "Network error: unable to reach the API.", fieldErrors: {}, cause: error };
  }
  if (error instanceof Error && error.message) {
    return { kind: "unknown", message: error.message, fieldErrors: {}, cause: error };
  }
  if (typeof error === "string" && error) {
    return { kind: "unknown", message: error, fieldErrors: {}, cause: error };
  }
  return { kind: "unknown", message: "Unexpected error communicating with the API.", fieldErrors: {}, cause: error };
}

type FormEntry = [string, string | Blob];

interface RawRequestOptions extends BaseRequestOptions {
  params?: Record<string, unknown>;
  query?: Record<string, unknown>;
  body?: unknown;
}

function errorName(error: unknown): unknown {
  return error !== null && typeof error === "object" ? (error as { name?: unknown }).name : undefined;
}

function readStringField(data: unknown, key: string): string {
  if (data === null || typeof data !== "object") return "";
  const value = (data as Record<string, unknown>)[key];
  return typeof value === "string" ? value : "";
}

function issueField(e: Record<string, unknown>): string {
  if (Array.isArray(e["loc"])) return e["loc"].map(String).join(".");
  if (typeof e["field"] === "string") return e["field"];
  if (typeof e["path"] === "string") return e["path"];
  return "";
}

function issueMessage(e: Record<string, unknown>): string {
  if (typeof e["msg"] === "string") return e["msg"];
  if (typeof e["message"] === "string") return e["message"];
  if (typeof e["error"] === "string") return e["error"];
  return "";
}

function addFieldError(out: Record<string, string[]>, field: string, message: string): void {
  const list = out[field];
  if (list) list.push(message);
  else out[field] = [message];
}

function extractFieldErrors(data: unknown): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  if (data === null || typeof data !== "object") return out;
  const d = data as Record<string, unknown>;
  const source = d["errors"] ?? d["detail"];
  if (Array.isArray(source)) {
    for (const item of source) {
      if (item === null || typeof item !== "object") continue;
      const e = item as Record<string, unknown>;
      const field = issueField(e);
      const message = issueMessage(e);
      if (field && message) addFieldError(out, field, message);
    }
  } else if (source !== null && typeof source === "object") {
    for (const [field, value] of Object.entries(source)) {
      if (typeof value === "string") {
        addFieldError(out, field, value);
      } else if (Array.isArray(value)) {
        for (const item of value) {
          if (typeof item === "string") addFieldError(out, field, item);
        }
      }
    }
  }
  return out;
}

function formatValidationErrors(items: unknown[]): string {
  const parts = items.map((item) => {
    if (typeof item === "string") return item;
    if (item && typeof item === "object") {
      const e = item as Record<string, unknown>;
      const loc = issueField(e);
      const msg = issueMessage(e);
      if (loc && msg) return loc + ": " + msg;
      if (msg) return msg;
    }
    return JSON.stringify(item);
  });
  return "Validation error: " + parts.join("; ");
}

function errorMessage(data: unknown, fallback: string): string {
  if (typeof data === "string" && data.trim()) return data.trim();
  if (data && typeof data === "object") {
    const d = data as Record<string, unknown>;
    if (typeof d["message"] === "string" && d["message"]) return d["message"];
    if (typeof d["error"] === "string" && d["error"]) return d["error"];
    if (typeof d["detail"] === "string" && d["detail"]) return d["detail"];
    if (Array.isArray(d["detail"]) && d["detail"].length > 0) return formatValidationErrors(d["detail"]);
    if (Array.isArray(d["errors"]) && d["errors"].length > 0) return formatValidationErrors(d["errors"]);
    if (typeof d["title"] === "string" && d["title"]) return d["title"];
  }
  return fallback;
}

function isBlob(value: unknown): value is Blob {
  return (
    (typeof Blob !== "undefined" && value instanceof Blob) ||
    (typeof File !== "undefined" && value instanceof File)
  );
}

function isReadableStream(value: unknown): value is ReadableStream {
  return (
    (typeof ReadableStream !== "undefined" && value instanceof ReadableStream) ||
    (value !== null && typeof value === "object" && typeof (value as { getReader?: unknown }).getReader === "function")
  );
}

function isBodyInit(body: unknown): body is BodyInit {
  return (
    (typeof FormData !== "undefined" && body instanceof FormData) ||
    (typeof URLSearchParams !== "undefined" && body instanceof URLSearchParams) ||
    isBlob(body) ||
    isReadableStream(body) ||
    (typeof ArrayBuffer !== "undefined" && (body instanceof ArrayBuffer || ArrayBuffer.isView(body)))
  );
}

function appendQueryParam(search: URLSearchParams, key: string, value: unknown): void {
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

function collectEntries(key: string, value: unknown, out: FormEntry[]): void {
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

function toEntries(source: object): FormEntry[] {
  const out: FormEntry[] = [];
  for (const [key, value] of Object.entries(source)) collectEntries(key, value, out);
  return out;
}

function hasBlobValue(value: unknown, seen = new Set<unknown>()): boolean {
  if (value === null || typeof value !== "object") return false;
  if (isBlob(value)) return true;
  if (seen.has(value)) return false;
  seen.add(value);
  if (Array.isArray(value)) return value.some((item) => hasBlobValue(item, seen));
  if (value instanceof Date) return false;
  return Object.values(value).some((item) => hasBlobValue(item, seen));
}

function resolvePath(endpoint: string, template: string, params: Record<string, unknown>): string {
  return template
    .split("/")
    .map((segment) => {
      if (!segment.startsWith("{") || !segment.endsWith("}") || segment.length <= 2) {
        return segment;
      }
      const inner = segment.slice(1, -1);
      const colonIdx = inner.indexOf(":");
      const name = colonIdx >= 0 ? inner.slice(0, colonIdx) : inner;
      const value = params[name];
      if (value === undefined || value === null) {
        throw new Error("Missing required path parameter \"" + name + "\" for " + endpoint);
      }
      return encodeURIComponent(String(value));
    })
    .join("/");
}

function encodeBody(body: unknown, headers: Headers, encoding: BodyEncoding): BodyInit {
  if (typeof FormData !== "undefined" && body instanceof FormData) {
    headers.delete("Content-Type"); // let fetch set the multipart boundary
    return body;
  }
  if (isBodyInit(body)) return body;
  const contentType = headers.get("Content-Type") ?? "";
  if (body !== null && typeof body === "object") {
    if (encoding === "multipart" || contentType.includes("multipart/form-data") || hasBlobValue(body)) {
      const form = new FormData();
      for (const [key, value] of toEntries(body)) form.append(key, value);
      headers.delete("Content-Type"); // let fetch set the multipart boundary
      return form;
    }
    if (encoding === "form" || contentType.includes("application/x-www-form-urlencoded")) {
      const form = new URLSearchParams();
      for (const [key, value] of toEntries(body)) {
        if (typeof value === "string") form.append(key, value);
      }
      return form;
    }
  }
  if (typeof body === "string" && (encoding === "raw" || (contentType !== "" && !contentType.includes("json")))) {
    return body;
  }
  if (!contentType) headers.set("Content-Type", "application/json");
  return JSON.stringify(body);
}

function createTimeoutSignal(timeoutMs: number | undefined): AbortSignal | undefined {
  if (timeoutMs === undefined || timeoutMs <= 0) return undefined;
  if (typeof AbortSignal !== "undefined" && typeof AbortSignal.timeout === "function") {
    return AbortSignal.timeout(timeoutMs);
  }
  if (typeof AbortController === "undefined") return undefined;
  const controller = new AbortController();
  const timeoutReason =
    typeof DOMException !== "undefined"
      ? new DOMException("The operation timed out.", "TimeoutError")
      : Object.assign(new Error("The operation timed out."), { name: "TimeoutError" });
  const timer: unknown = setTimeout(() => controller.abort(timeoutReason), timeoutMs);
  if (
    timer &&
    typeof timer === "object" &&
    "unref" in timer &&
    typeof (timer as { unref?: unknown }).unref === "function"
  ) {
    (timer as { unref: () => void }).unref();
  }
  return controller.signal;
}

function combineSignals(a: AbortSignal | undefined, b: AbortSignal | undefined): AbortSignal | undefined {
  if (!a) return b;
  if (!b) return a;
  const any = (AbortSignal as { any?: (signals: AbortSignal[]) => AbortSignal }).any;
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

function retryDelay(baseDelay: number, attempt: number, response?: Response): number {
  let delay = baseDelay * Math.pow(2, attempt - 1) * (0.5 + Math.random() * 0.5);
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

/** Waits for the retry backoff, rejecting early when the request is cancelled or times out. */
function sleep(ms: number, signal: AbortSignal | null | undefined): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason);
      return;
    }
    const onAbort = () => {
      clearTimeout(timer);
      reject(signal?.reason);
    };
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

async function readErrorBody(res: Response): Promise<unknown> {
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

async function readBody(res: Response): Promise<unknown> {
  if (res.status === 204 || res.status === 205 || res.headers.get("content-length") === "0") return undefined;
  const contentType = res.headers.get("content-type") ?? "";
  if (contentType.includes("application/json") || contentType.includes("+json")) {
    if (typeof res.text === "function") {
      const text = await res.text();
      return text ? JSON.parse(text) : undefined;
    }
    return res.json();
  }
  if (contentType.includes("text/event-stream") && res.body) {
    return typeof TextDecoderStream !== "undefined" ? res.body.pipeThrough(new TextDecoderStream()) : res.body;
  }
  const isBinary = /^(image|audio|video)\/|application\/(octet-stream|pdf|zip|gzip|vnd\.|x-)/.test(contentType);
  if (isBinary && typeof res.blob === "function") return res.blob();
  if (contentType.startsWith("text/") || contentType.includes("xml")) return res.text();
  if (typeof res.blob === "function") return res.blob();
  return res.text();
}

/**
 * Creates a client for any generated endpoint map. Generated SDKs expose a pre-bound
 * createGotaClient; use this directly only to target several APIs with one runtime.
 */
export function createGotaClientFor<E extends EndpointMap<E>>(
  baseUrl = "",
  clientOptions: GotaClientOptions = {},
  bodyEncodings: BodyEncodingMap = {},
): GotaClientFor<E> {
  const root = baseUrl.replace(/\/+$/, "");

  async function request(endpoint: string, options: RawRequestOptions): Promise<unknown> {
    const separator = endpoint.indexOf(" ");
    const method = endpoint.slice(0, separator);
    const template = endpoint.slice(separator + 1);
    const path = resolvePath(endpoint, template, options.params ?? {});

    let url = root + (path.startsWith("/") ? path : "/" + path);
    if (options.query) {
      const search = new URLSearchParams();
      for (const [key, value] of Object.entries(options.query)) appendQueryParam(search, key, value);
      const qs = search.toString();
      if (qs) url += (url.includes("?") ? "&" : "?") + qs;
    }

    const baseHeaders = typeof clientOptions.headers === "function" ? await clientOptions.headers() : clientOptions.headers;
    const headers = new Headers(baseHeaders);
    if (options.headers) {
      for (const [key, value] of Object.entries(options.headers)) headers.set(key, String(value));
    }

    const init: GotaRequestInit = { method, headers };
    if (options.body !== undefined) {
      init.body = encodeBody(options.body, headers, bodyEncodings[endpoint] ?? "json");
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
    const duplex = options.duplex ?? (isReadableStream(init.body) ? "half" : undefined);
    if (duplex) init.duplex = duplex;

    const signal = combineSignals(options.signal, createTimeoutSignal(options.timeoutMs ?? clientOptions.timeoutMs));
    if (signal) init.signal = signal;

    if (clientOptions.onRequest) await clientOptions.onRequest({ endpoint, url, init });
    if (options.onRequest) await options.onRequest({ endpoint, url, init });

    const context: GotaResponseContext = { endpoint, url };
    const fail = async (err: unknown): Promise<never> => {
      // Cancellations requested by the caller (e.g. effect cleanup) are expected: skip onError hooks.
      if (!options.signal?.aborted) {
        const errObj = err instanceof Error ? err : new Error(String(err));
        if (clientOptions.onError) await clientOptions.onError(errObj, context);
        if (options.onError) await options.onError(errObj, context);
      }
      throw err;
    };

    const retryOpt = options.retry ?? clientOptions.retry;
    let maxAttempts = 1;
    let baseDelay = 300;
    let retryStatuses = DEFAULT_RETRY_STATUSES;
    let retryMethods = DEFAULT_RETRY_METHODS;
    if (typeof retryOpt === "number" && retryOpt > 0) {
      maxAttempts = 1 + retryOpt;
    } else if (typeof retryOpt === "object" && retryOpt !== null) {
      if (typeof retryOpt.attempts === "number" && retryOpt.attempts > 0) maxAttempts = 1 + retryOpt.attempts;
      if (typeof retryOpt.delayMs === "number" && retryOpt.delayMs >= 0) baseDelay = retryOpt.delayMs;
      if (Array.isArray(retryOpt.statusCodes)) retryStatuses = retryOpt.statusCodes;
      if (Array.isArray(retryOpt.methods)) retryMethods = retryOpt.methods.map((m) => m.toUpperCase());
    }
    if (!retryMethods.includes(method.toUpperCase())) maxAttempts = 1;

    const fetchFn = clientOptions.fetch ?? globalThis.fetch.bind(globalThis);
    let res: Response;
    let attempt = 0;
    while (true) {
      try {
        res = await fetchFn(url, init);
      } catch (fetchErr) {
        const aborted = init.signal?.aborted === true;
        if (!aborted && fetchErr !== null && typeof fetchErr === "object") NETWORK_ERRORS.add(fetchErr);
        if (attempt < maxAttempts - 1 && !aborted) {
          attempt++;
          try {
            await sleep(retryDelay(baseDelay, attempt), init.signal);
          } catch (abortErr) {
            return fail(abortErr);
          }
          continue;
        }
        return fail(fetchErr);
      }
      if (!res.ok && attempt < maxAttempts - 1 && retryStatuses.includes(res.status) && !init.signal?.aborted) {
        attempt++;
        res.body?.cancel().catch(() => undefined); // release the connection before retrying
        try {
          await sleep(retryDelay(baseDelay, attempt, res), init.signal);
        } catch (abortErr) {
          return fail(abortErr);
        }
        continue;
      }
      break;
    }

    if (clientOptions.onResponse) await clientOptions.onResponse(res, context);
    if (options.onResponse) await options.onResponse(res, context);

    if (!res.ok) {
      const data = await readErrorBody(res);
      return fail(new GotaHttpError(res.status, res.statusText, data, res.headers, res, endpoint));
    }
    return readBody(res);
  }

  const client: GotaClientFor<E> = {
    baseUrl: root,
    fetch(endpoint, ...args) {
      const [options] = args as unknown as [RawRequestOptions?];
      return request(String(endpoint), options ?? {}) as Promise<E[typeof endpoint]["response"]>;
    },
    queryOptions(endpoint, ...args) {
      const [options] = args as unknown as [RawRequestOptions?];
      const base: RawRequestOptions = options ?? {};
      const keyArgs: GotaQueryKeyArgs = {};
      if (base.params !== undefined) keyArgs.params = base.params;
      if (base.query !== undefined) keyArgs.query = base.query;
      if (base.body !== undefined) keyArgs.body = base.body;
      return {
        queryKey: [endpoint, keyArgs] as const,
        queryFn: (queryContext?: { signal?: AbortSignal | undefined }) => {
          const signal = combineSignals(base.signal, queryContext?.signal);
          const merged: RawRequestOptions = signal ? { ...base, signal } : base;
          return request(String(endpoint), merged) as Promise<E[typeof endpoint]["response"]>;
        },
      };
    },
  };
  return client;
}
`
