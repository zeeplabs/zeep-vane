import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { Incident, IncidentStatus, IncidentUpdate } from "../../lib/mockData";

export function useIncidents() {
  return useQuery({
    queryKey: ["incidents"],
    queryFn: () => apiFetch<Incident[]>("/api/incidents"),
  });
}

export interface CreateIncidentInput {
  title: string;
  service_ids: string[];
}

export function useCreateIncident() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateIncidentInput) =>
      apiFetch<Incident>("/api/incidents", { method: "POST", body: JSON.stringify(input) }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["incidents"] });
    },
  });
}

export function useIncidentUpdates(incidentId: string) {
  return useQuery({
    queryKey: ["incidents", incidentId, "updates"],
    queryFn: () => apiFetch<IncidentUpdate[]>(`/api/incidents/${incidentId}/updates`),
    enabled: Boolean(incidentId),
  });
}

export function useAddIncidentUpdate(incidentId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: string) =>
      apiFetch<IncidentUpdate[]>(`/api/incidents/${incidentId}/updates`, {
        method: "POST",
        body: JSON.stringify({ body }),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["incidents", incidentId, "updates"] });
    },
  });
}

export function useTransitionIncident(incidentId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (status: IncidentStatus) =>
      apiFetch<Incident>(`/api/incidents/${incidentId}`, {
        method: "PATCH",
        body: JSON.stringify({ status }),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["incidents"] });
    },
  });
}
