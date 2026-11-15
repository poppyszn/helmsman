import { describe, expect, it } from "vitest";
import { catalogEnvNames } from "./catalog";

const sampleApps = [
  { id: "billing", environments: [{ env: "staging" }, { env: "prod" }, { env: "develop" }] },
  { id: "web", environments: [{ env: "staging" }] },
];

describe("catalogEnvNames", () => {
  it("collects unique sorted environments across all apps for '*'", () => {
    expect(catalogEnvNames(sampleApps, "*")).toEqual(["develop", "prod", "staging"]);
  });

  it("limits environments to the selected app", () => {
    expect(catalogEnvNames(sampleApps, "web")).toEqual(["staging"]);
    expect(catalogEnvNames(sampleApps, "billing")).toEqual(["develop", "prod", "staging"]);
  });

  it("returns empty for unknown apps or empty catalogs", () => {
    expect(catalogEnvNames(sampleApps, "missing")).toEqual([]);
    expect(catalogEnvNames([], "*")).toEqual([]);
  });
});
