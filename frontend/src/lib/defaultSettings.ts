import type { SettingsValues } from "./api";

export const defaultSettings: SettingsValues = {
  themePreference: "system",
  startupView: "launcher",
  notifications: "enabled",
  defaultProvider: "",
  defaultModel: "",
  reasoningMode: "balanced",
  sessionMemory: "enabled",
  textIndex: "fts5",
  vectorIndex: "deferred",
  shellExecution: "approval",
  pythonWorker: "available",
  gpuProviders: "disabled",
  shellPolicy: "approve",
  writePolicy: "approve",
  networkPolicy: "approve",
  databaseRetention: "keep",
  secretStorage: "keychain",
  referencePolicy: "ignored",
  hostGuard: "localhost-only",
  originGuard: "strict",
  scienceNetwork: "live-only"
};
