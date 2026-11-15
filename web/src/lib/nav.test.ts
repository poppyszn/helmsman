import { describe, expect, it } from "vitest";
import { NAV_ITEMS, visibleNavItems } from "./nav";

describe("visibleNavItems", () => {
  it("shows only Apps when the principal is missing or has no special flags", () => {
    expect(visibleNavItems().map((x) => x.to)).toEqual(["/"]);
    expect(
      visibleNavItems({ can: { admin: false, audit: false, agents: false, approve: false } }).map((x) => x.to)
    ).toEqual(["/"]);
  });

  it("includes approvals, agents, and audit destinations according to permissions", () => {
    expect(
      visibleNavItems({ can: { admin: false, audit: false, agents: false, approve: true } }).map((x) => x.to)
    ).toEqual(["/", "/approvals"]);

    expect(
      visibleNavItems({ can: { admin: false, audit: true, agents: true, approve: false } }).map((x) => x.to)
    ).toEqual(["/", "/admin/agents", "/admin/audit"]);
  });

  it("exposes all destinations for a full site admin, including Setup check and Notifications", () => {
    const routes = visibleNavItems({ can: { admin: true, audit: true, agents: true, approve: true } }).map((x) => x.to);
    expect(routes).toEqual([
      "/",
      "/admin/users",
      "/admin/bindings",
      "/admin/catalog",
      "/approvals",
      "/admin/github",
      "/admin/integrations",
      "/admin/notifications",
      "/admin/doctor",
      "/admin/agents",
      "/admin/audit",
    ]);
  });

  it("keeps ids and paths unique across NAV_ITEMS", () => {
    expect(new Set(NAV_ITEMS.map((x) => x.id)).size).toBe(NAV_ITEMS.length);
    expect(new Set(NAV_ITEMS.map((x) => x.to)).size).toBe(NAV_ITEMS.length);
  });
});
