import { describe, it, expect } from "vitest";
import type { DomainSSLStatus, DomainStatus } from "../../types/api";
import {
  domainStatusLabel,
  domainStatusVariant,
  domainStatusDotColor,
  sslStatusLabel,
  sslStatusColor,
} from "./domainStatusMeta";

const domainStatuses: DomainStatus[] = ["pending", "verified", "error"];
const sslStatuses: DomainSSLStatus[] = ["pending", "active", "error"];

describe("domainStatusMeta", () => {
  it("domainStatusLabel/domainStatusVariant/domainStatusDotColor têm as 3 chaves de DomainStatus", () => {
    for (const status of domainStatuses) {
      expect(domainStatusLabel[status]).toBeTypeOf("string");
      expect(domainStatusVariant[status]).toBeTypeOf("string");
      expect(domainStatusDotColor[status]).toBeTypeOf("string");
    }
    expect(Object.keys(domainStatusLabel).sort()).toEqual([...domainStatuses].sort());
    expect(Object.keys(domainStatusVariant).sort()).toEqual([...domainStatuses].sort());
    expect(Object.keys(domainStatusDotColor).sort()).toEqual([...domainStatuses].sort());
  });

  it("sslStatusLabel/sslStatusColor têm as 3 chaves de DomainSSLStatus", () => {
    for (const status of sslStatuses) {
      expect(sslStatusLabel[status]).toBeTypeOf("string");
      expect(sslStatusColor[status]).toBeTypeOf("string");
    }
    expect(Object.keys(sslStatusLabel).sort()).toEqual([...sslStatuses].sort());
    expect(Object.keys(sslStatusColor).sort()).toEqual([...sslStatuses].sort());
  });
});
