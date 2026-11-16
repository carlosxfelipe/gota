/**
 * Auto-generated TypeScript SDK. Do not edit by hand.
 * This directory is managed by TypeScript SDK codegen: files are overwritten and stale ones removed on every regeneration.
 * Keep customizations in a wrapper module outside of this directory.
 */
/* eslint-disable */

export interface CreateTodoRequest {
  /**
   * Task title
   * @example Review pull requests
   */
  title: string;
  /**
   * Task priority
   * @example high
   * @default normal
   */
  priority?: "low" | "normal" | "high";
}

export interface ErrorResponse {
  error: string;
  status: number;
  requestId?: string;
}

export interface PatchTodoRequest {
  /**
   * Updated task title
   * @example Deploy release v2.1
   */
  title?: string;
  /**
   * Updated task priority
   * @example high
   */
  priority?: "low" | "normal" | "high";
  /**
   * Updated completion status
   * @example true
   */
  done?: boolean;
}

export interface TodoResponse {
  /**
   * Todo ID
   * @example 1
   */
  id: number;
  /**
   * Task title
   * @example Review pull requests
   */
  title: string;
  /**
   * Task priority
   * @example normal
   */
  priority: "low" | "normal" | "high";
  /**
   * Completion status
   * @example false
   */
  done: boolean;
}

export interface UpdateTodoRequest {
  /**
   * Task title
   * @example Review pull requests
   */
  title: string;
  /**
   * Task priority
   * @example high
   */
  priority: "low" | "normal" | "high";
  /**
   * Completion status
   * @example true
   */
  done?: boolean;
}

export interface ValidationError {
  message: string;
  errors: Record<string, string[]>;
  status: number;
}
