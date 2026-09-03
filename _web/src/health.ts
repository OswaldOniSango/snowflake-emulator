/** Result of probing the emulator's /health endpoint. */
export type HealthState =
  | { status: "ok" }
  | { status: "down"; detail: string };

/**
 * Probes the emulator. The UI is served by the emulator itself, so a failure
 * here means the process is up but unhealthy — worth showing, not hiding.
 */
export async function checkHealth(
  fetchFn: typeof fetch = fetch,
): Promise<HealthState> {
  try {
    const response = await fetchFn("/health");
    if (!response.ok) {
      return { status: "down", detail: `HTTP ${response.status}` };
    }
    return { status: "ok" };
  } catch (cause) {
    return {
      status: "down",
      detail: cause instanceof Error ? cause.message : "request failed",
    };
  }
}
