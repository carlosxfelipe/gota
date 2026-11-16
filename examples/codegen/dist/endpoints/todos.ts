/**
 * Auto-generated TypeScript SDK. Do not edit by hand.
 * This directory is managed by TypeScript SDK codegen: files are overwritten and stale ones removed on every regeneration.
 * Keep customizations in a wrapper module outside of this directory.
 */
/* eslint-disable */

import type { CreateTodoRequest, ErrorResponse, PatchTodoRequest, TodoResponse, UpdateTodoRequest, ValidationError } from "../types/todos";

export interface TodosEndpoints {
  /**
   * Delete a todo
   * Deletes an existing todo item by its unique identifier.
   */
  "DELETE /todos/{id}": {
    method: "DELETE";
    path: "/todos/{id}";
    params: { id: number };
    query: void;
    body: void;
    response: void;
    error: ErrorResponse | ValidationError;
    request: { params: { id: number }; query?: never; body?: never };
  };
  /**
   * List all todos
   * Returns all todo items in the store, with optional filtering by completion status and priority.
   */
  "GET /todos": {
    method: "GET";
    path: "/todos";
    params: void;
    query: { done?: boolean; priority?: "low" | "normal" | "high" };
    body: void;
    response: TodoResponse[];
    error: ErrorResponse | ValidationError;
    request: { params?: never; query?: { done?: boolean; priority?: "low" | "normal" | "high" }; body?: never };
  };
  /**
   * Get todo by ID
   * Retrieves a single todo item by its unique identifier.
   */
  "GET /todos/{id}": {
    method: "GET";
    path: "/todos/{id}";
    params: { id: number };
    query: void;
    body: void;
    response: TodoResponse;
    error: ErrorResponse | ValidationError;
    request: { params: { id: number }; query?: never; body?: never };
  };
  /**
   * Partially update a todo
   * Updates only the provided fields of an existing todo item.
   */
  "PATCH /todos/{id}": {
    method: "PATCH";
    path: "/todos/{id}";
    params: { id: number };
    query: void;
    body: PatchTodoRequest;
    response: TodoResponse;
    error: ErrorResponse | ValidationError;
    request: { params: { id: number }; query?: never; body: PatchTodoRequest };
  };
  /**
   * Create a new todo
   * Validates the input payload and creates a new todo item.
   */
  "POST /todos": {
    method: "POST";
    path: "/todos";
    params: void;
    query: void;
    body: CreateTodoRequest;
    response: TodoResponse;
    error: ErrorResponse | ValidationError;
    request: { params?: never; query?: never; body: CreateTodoRequest };
  };
  /**
   * Replace a todo
   * Validates the full payload and replaces an existing todo item.
   */
  "PUT /todos/{id}": {
    method: "PUT";
    path: "/todos/{id}";
    params: { id: number };
    query: void;
    body: UpdateTodoRequest;
    response: TodoResponse;
    error: ErrorResponse | ValidationError;
    request: { params: { id: number }; query?: never; body: UpdateTodoRequest };
  };
}
