/**
 * Auto-generated TypeScript SDK. Do not edit by hand.
 * This directory is managed by TypeScript SDK codegen: files are overwritten and stale ones removed on every regeneration.
 * Keep customizations in a wrapper module outside of this directory.
 */
/* eslint-disable */

export interface HealthStatus {
  /**
   * Service health state
   * @example ok
   */
  status: string;
  /**
   * Process uptime in seconds
   * @example 42
   */
  uptimeSeconds: number;
  /**
   * Go runtime version
   * @example go1.27.4
   */
  goVersion: string;
}
