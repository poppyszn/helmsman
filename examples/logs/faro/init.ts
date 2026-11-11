// The same setup for an app built with a bundler (Vite, webpack, ...). Install the SDK first:
//   npm install @grafana/faro-web-sdk
// and import this file once, as early as possible in your entry point.
import { initializeFaro, getWebInstrumentations, LogLevel } from "@grafana/faro-web-sdk";

export const faro = initializeFaro({
  // Your Helmsman server's /faro/collect, or a Faro-only Alloy. Set it per build, not in the source.
  url: import.meta.env.VITE_FARO_URL,

  // app.name must equal logs.faro_app in the catalog, and app.environment the environment's name.
  app: {
    name: "billing-web",
    version: import.meta.env.VITE_APP_VERSION, // shown next to every line in Helmsman, so use the release
    environment: import.meta.env.VITE_ENV, // "staging" or "prod"
  },

  instrumentations: [...getWebInstrumentations({ captureConsole: true, captureConsoleDisabledLevels: [LogLevel.DEBUG, LogLevel.TRACE] })],

  // Skip errors you cannot act on.
  ignoreErrors: [/ResizeObserver loop/, /Script error\./],

  // Drop query strings and anything else personal before it leaves the browser.
  beforeSend: (item) => {
    if (item.meta?.page?.url) item.meta.page.url = item.meta.page.url.split("?")[0];
    return item;
  },
});

// Report your own events and messages:
//   faro.api.pushEvent("checkout_started", { items: "3" });
//   faro.api.pushLog(["payment retried"], { level: LogLevel.WARN });
// and caught errors that would otherwise be swallowed:
//   faro.api.pushError(error);
