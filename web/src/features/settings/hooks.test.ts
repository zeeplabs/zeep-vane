import { describe, it, expect } from "vitest";
import { http, HttpResponse } from "msw";
import { renderHook, waitFor } from "@testing-library/react";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { useCompanySettings, useUpdateCompanySettings, useUploadCompanyLogo } from "./hooks";

function testLogoFile(): File {
  return new File(["fake-png-bytes"], "logo.png", { type: "image/png" });
}

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("company settings hooks", () => {
  it("useCompanySettings retorna os dados persistidos da fixture", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useCompanySettings(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data!.name).toBe("Sua Empresa Ltda.");
    expect(result.current.data!.contact_email).toBe("contato@suaempresa.com");
  });

  it("useUpdateCompanySettings envia apenas name e contact_email e atualiza o cache", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ settings: useCompanySettings(), update: useUpdateCompanySettings() }),
      { wrapper: TestQueryProvider },
    );
    await waitFor(() => expect(result.current.settings.isSuccess).toBe(true));

    const updated = await result.current.update.mutateAsync({
      name: "Nova Empresa",
      contact_email: "novo@empresa.com",
    });

    expect(updated.name).toBe("Nova Empresa");
    expect(updated.contact_email).toBe("novo@empresa.com");
    await waitFor(() => expect(result.current.settings.data!.name).toBe("Nova Empresa"));
  });

  it("useUpdateCompanySettings propaga erro 422 sem atualizar o cache", async () => {
    server.use(
      http.patch(
        "/api/company-settings",
        () =>
          HttpResponse.json(
            { error: "name is required and contact_email must be a valid e-mail address" },
            { status: 422 },
          ),
      ),
    );
    await loginAsOwner();
    const { result } = renderHook(() => useUpdateCompanySettings(), { wrapper: TestQueryProvider });

    await expect(
      result.current.mutateAsync({ name: "", contact_email: "not-an-email" }),
    ).rejects.toMatchObject({ status: 422 });
  });

  it("useUploadCompanyLogo envia FormData e atualiza logo_url no cache", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ settings: useCompanySettings(), upload: useUploadCompanyLogo() }),
      { wrapper: TestQueryProvider },
    );
    await waitFor(() => expect(result.current.settings.isSuccess).toBe(true));

    const file = testLogoFile();
    const updated = await result.current.upload.mutateAsync(file);

    expect(updated.logo_url).toBe("/uploads/logo.png");
    await waitFor(() => expect(result.current.settings.data!.logo_url).toBe("/uploads/logo.png"));
  });

  it("useUploadCompanyLogo propaga erro 500 sem atualizar logo_url", async () => {
    server.use(
      http.post(
        "/api/company-settings/logo",
        () => HttpResponse.json({ error: "internal server error" }, { status: 500 }),
      ),
    );
    await loginAsOwner();
    const { result } = renderHook(() => useUploadCompanyLogo(), { wrapper: TestQueryProvider });

    const file = testLogoFile();
    await expect(result.current.mutateAsync(file)).rejects.toMatchObject({ status: 500 });
  });
});
