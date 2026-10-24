import { useLayoutEffect } from "react"
import { usePreferences } from "@/stores/preferences"

export function useAppearance() {
  const theme = usePreferences((state) => state.theme)
  const accent = usePreferences((state) => state.accent)
  useLayoutEffect(() => {
    const media = window.matchMedia("(prefers-color-scheme: dark)")
    const update = () => {
      const dark = theme === "dark" || (theme === "system" && media.matches)
      document.documentElement.classList.toggle("dark", dark)
      document.documentElement.style.colorScheme = dark ? "dark" : "light"
    }
    update()
    media.addEventListener("change", update)
    return () => media.removeEventListener("change", update)
  }, [theme])
  useLayoutEffect(() => {
    document.documentElement.dataset.accent = accent
  }, [accent])
}
