import { describe, it, expect, vi } from "vitest";
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

    expect(updated.logo_url).toBe("/uploads/logo");
    await waitFor(() => expect(result.current.settings.data!.logo_url).toBe("/uploads/logo"));
  });

  it("useUploadCompanyLogo monta o FormData com a chave 'logo' esperada pelo backend", async () => {
    // O contrato entre camadas (nome do campo multipart) é um literal
    // independente em cada lado: hooks.ts hardcoda "logo" no FormData, o Go
    // hardcoda logoFormFieldName = "logo". O MSW não consegue ler o corpo
    // multipart sob jsdom (request.formData()/request.text() travam - ver
    // validation.md, Non-Shallow Litmus), então este teste inspeciona o
    // FormData que o hook monta diretamente, via um spy em fetch, sem
    // depender do MSW ler o corpo.
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    await loginAsOwner();
    const { result } = renderHook(() => useUploadCompanyLogo(), { wrapper: TestQueryProvider });

    const file = testLogoFile();
    await result.current.mutateAsync(file);

    const uploadCall = fetchSpy.mock.calls.find(([url]) => String(url).includes("/api/company-settings/logo"));
    expect(uploadCall).toBeDefined();
    const [, init] = uploadCall!;
    expect(init?.body).toBeInstanceOf(FormData);
    expect((init!.body as FormData).get("logo")).toBe(file);

    fetchSpy.mockRestore();
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
