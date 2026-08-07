export type StartupRoute = "onboarding" | "launcher";

export type StartupReadiness = {
  serverStatus: string;
  providerId: string;
  modelId: string;
  providerStatus: string;
};

export function deriveStartupRoute(readiness: StartupReadiness): StartupRoute {
  const providerReady =
    readiness.serverStatus === "ready" &&
    readiness.providerId.trim() !== "" &&
    readiness.modelId.trim() !== "" &&
    readiness.providerStatus === "ready";
  return providerReady ? "launcher" : "onboarding";
}
