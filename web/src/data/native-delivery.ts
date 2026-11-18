import { useInfiniteQuery, useMutation, useQuery } from "@tanstack/react-query";
import { useRef } from "react";
import { api, operationHeaders, read } from "./client";
import type { Schema } from "./client";
import { useRemoteKey } from "./session";
import { newId } from "../lib/id";

export function usePublishDelivery() {
  const attempt = useRef<{ signature: string; key: string } | undefined>(undefined);
  return useMutation({
    mutationFn: (body: Schema<"NativeDeliveryPublish">) => {
      const signature = JSON.stringify(body);
      if (attempt.current?.signature !== signature) attempt.current = { signature, key: newId() };
      return read(api.POST("/device/native-deliveries", {
        params: { header: operationHeaders(attempt.current.key) }, body,
      }));
    },
    onSuccess: () => { attempt.current = undefined; },
  });
}

export function useDeviceDeliveries(device: string) {
  const scope = useRemoteKey();
  return useInfiniteQuery({
    queryKey: [...scope, "device-deliveries", device], enabled: !!device,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => read(api.GET("/device/apply-tasks", {
      params: { query: { targetDeviceId: device, protocolVersion: 2, cursor: pageParam, limit: 20 } }, signal,
    })),
    getNextPageParam: last => last.nextCursor || undefined,
  });
}

export function useDeliveryReceipt(id: string) {
  const scope = useRemoteKey();
  return useQuery({
    queryKey: [...scope, "delivery", id], enabled: !!id,
    queryFn: ({ signal }) => read(api.GET("/device/apply-tasks/{id}", { params: { path: { id } }, signal })),
    refetchInterval: query => ["pending", "claimed"].includes(query.state.data?.state ?? "") ? 3000 : false,
  });
}

// Keep an unacknowledged pairing request only in the caller's memory. A retry
// needs the same secret and request key if the registration response was lost.
export function createPairingAttempt() {
  const bytes = crypto.getRandomValues(new Uint8Array(32));
  const secret = btoa(String.fromCharCode(...bytes)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return { secret, key: newId() };
}

export function registerDevice(deviceId: string, attempt: ReturnType<typeof createPairingAttempt>) {
  return read(api.POST("/device/credentials", {
    params: { header: operationHeaders(attempt.key) },
    body: { deviceId, label: deviceId, credentialSecret: attempt.secret },
  }));
}
