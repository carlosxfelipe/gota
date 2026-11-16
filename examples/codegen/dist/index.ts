/**
 * Auto-generated TypeScript SDK. Do not edit by hand.
 * API: Gota API (1.0.0)
 * Simple API for managing todo items.
 * This directory is managed by TypeScript SDK codegen: files are overwritten and stale ones removed on every regeneration.
 * Keep customizations in a wrapper module outside of this directory.
 */
/* eslint-disable */

import { createClientFor, isHttpError } from "./runtime";
import type { ApiClientFor, BodyEncodingMap, ClientOptions, HttpError, RequestOptionsFor } from "./runtime";
import type { SystemEndpoints } from "./endpoints/system";
import type { TodosEndpoints } from "./endpoints/todos";

export * from "./runtime";
export type * from "./types/system";
export type * from "./types/todos";
export type { SystemEndpoints, TodosEndpoints };

export interface ApiEndpoints
  extends SystemEndpoints,
    TodosEndpoints {}

export type EndpointKey = keyof ApiEndpoints;

export type RequestParams<K extends EndpointKey> = ApiEndpoints[K]["params"];
export type RequestQuery<K extends EndpointKey> = ApiEndpoints[K]["query"];
export type RequestBody<K extends EndpointKey> = ApiEndpoints[K]["body"];
export type ResponseData<K extends EndpointKey> = ApiEndpoints[K]["response"];
export type ResponseError<K extends EndpointKey> = ApiEndpoints[K]["error"];

export type RequestOptions<K extends EndpointKey> = RequestOptionsFor<ApiEndpoints, K>;

export type ApiClient = ApiClientFor<ApiEndpoints>;

const BODY_ENCODINGS: BodyEncodingMap = {};

/** Creates a typed client for this API. */
export function createClient(baseUrl = "", clientOptions: ClientOptions = {}): ApiClient {
  return createClientFor<ApiEndpoints>(baseUrl, clientOptions, BODY_ENCODINGS);
}

/** Narrows an error to the documented error payload of a specific endpoint. */
export function isEndpointError<K extends EndpointKey>(
  error: unknown,
  endpoint: K,
): error is HttpError<ResponseError<K>> {
  return isHttpError(error) && error.endpoint === endpoint;
}
