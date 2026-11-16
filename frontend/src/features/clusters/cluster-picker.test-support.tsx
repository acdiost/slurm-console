import type { ReactNode } from "react"
import {
  ClusterPickerNavigationProvider,
  ClusterPickerNavigationTarget,
} from "./cluster-picker"

export function ClusterPickerTestLayout({ children }: { children: ReactNode }) {
  return (
    <ClusterPickerNavigationProvider>
      <header>
        <ClusterPickerNavigationTarget />
      </header>
      <main>{children}</main>
    </ClusterPickerNavigationProvider>
  )
}
