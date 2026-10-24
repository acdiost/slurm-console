import { create } from "zustand"
import { persist } from "zustand/middleware"

export type Theme = "light" | "dark" | "system"
export type Accent = "blue" | "red"
export type Density = "standard" | "compact"
type Preferences = { theme: Theme; accent: Accent; density: Density }
type PreferenceStore = Preferences & {
  setTheme: (theme: Theme) => void
  setAccent: (accent: Accent) => void
  setDensity: (density: Density) => void
}

export function sanitizePreferences(value: unknown): Preferences {
  const data =
    value && typeof value === "object" ? (value as Partial<Preferences>) : {}
  return {
    theme:
      data.theme === "light" || data.theme === "dark" ? data.theme : "system",
    accent: data.accent === "red" ? "red" : "blue",
    density: data.density === "compact" ? "compact" : "standard",
  }
}

export const usePreferences = create<PreferenceStore>()(
  persist(
    (set) => ({
      theme: "system",
      accent: "blue",
      density: "standard",
      setTheme: (theme) => set({ theme }),
      setAccent: (accent) => set({ accent }),
      setDensity: (density) => set({ density }),
    }),
    {
      name: "slurm-console-preferences",
      version: 1,
      partialize: ({ theme, accent, density }) => ({ theme, accent, density }),
      merge: (persisted, current) => ({
        ...current,
        ...sanitizePreferences(persisted),
      }),
    }
  )
)
