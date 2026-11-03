import { useEffect } from "react"
import { create } from "zustand"
import { useQuery } from "@tanstack/react-query"
import { defaults, identitySchema, type Identity } from "./schema"
export const usePlatformIdentity = create<Identity>(() => ({ ...defaults }))
export function PlatformIdentityLoader() {
  const info = useQuery({
    queryKey: ["platform-public"],
    queryFn: async ({ signal }) => {
      const r = await fetch("/api/v1/platform-info", {
        signal,
        credentials: "omit",
        cache: "no-store",
      })
      if (!r.ok) throw new Error("Platform information unavailable")
      return identitySchema.parse((await r.json()).data)
    },
    retry: false,
    staleTime: 30000,
    refetchOnWindowFocus: true,
  })
  useEffect(() => {
    if (info.isError) usePlatformIdentity.setState(defaults)
    else if (info.data) usePlatformIdentity.setState(info.data)
  }, [info.data, info.isError])
  return null
}
