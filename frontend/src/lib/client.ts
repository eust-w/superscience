import { SuperscienceClient } from "./api";

const defaultServer =
  import.meta.env.VITE_SUPERSCIENCE_SERVER ?? "";

export const client = new SuperscienceClient(defaultServer);

type DesktopAppBinding = {
  ServerURL: () => Promise<string>;
  OpenProjectFolder: () => Promise<string>;
};

type DesktopRuntime = {
  EventsOn: (name: string, callback: (...args: unknown[]) => void) => unknown;
  EventsOff: (name: string) => void;
};

function desktopApp(): DesktopAppBinding | null {
  const candidate = window as unknown as { go?: { main?: { App?: DesktopAppBinding } } };
  return candidate.go?.main?.App ?? null;
}

function desktopRuntime(): DesktopRuntime | null {
  return (window as unknown as { runtime?: DesktopRuntime }).runtime ?? null;
}

export function isDesktop() {
  return desktopApp() !== null;
}

export async function configureClient() {
  const app = desktopApp();
  if (!app) return;
  const serverURL = await app.ServerURL();
  if (!/^http:\/\/(127\.0\.0\.1|localhost):\d+$/.test(serverURL)) {
    throw new Error("Desktop server returned an invalid localhost URL.");
  }
  client.setBase(serverURL);
}

export async function pickProjectFolder() {
  const app = desktopApp();
  if (!app) return "";
  return app.OpenProjectFolder();
}

export function onDesktopEvent(name: "desktop:open-project" | "desktop:error", callback: (value: string) => void) {
  const runtime = desktopRuntime();
  if (!runtime) return () => undefined;
  runtime.EventsOn(name, (...args) => callback(String(args[0] ?? "")));
  return () => runtime.EventsOff(name);
}
