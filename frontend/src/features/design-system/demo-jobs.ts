export type DemoJob = {
  id: string
  name: string
  user: string
  partition: string
  state: string
  cpus: number
  duration: string
}
// 仅用于设计系统验收，禁止当作真实集群查询结果复用。
export const demoJobs: DemoJob[] = [
  {
    id: "20481",
    name: "llama3-finetune",
    user: "zhang.wei",
    partition: "gpu-a100",
    state: "RUNNING",
    cpus: 32,
    duration: "02:18:42",
  },
  {
    id: "20482",
    name: "molecular-dynamics",
    user: "li.ming",
    partition: "compute",
    state: "RUNNING",
    cpus: 128,
    duration: "01:42:16",
  },
  {
    id: "20483",
    name: "protein-folding",
    user: "chen.yu",
    partition: "gpu-a100",
    state: "PENDING",
    cpus: 16,
    duration: "—",
  },
  {
    id: "20484",
    name: "climate-simulation",
    user: "wang.lin",
    partition: "compute",
    state: "COMPLETED",
    cpus: 64,
    duration: "04:32:08",
  },
  {
    id: "20485",
    name: "dataset-preprocess",
    user: "zhang.wei",
    partition: "compute",
    state: "FAILED",
    cpus: 8,
    duration: "00:03:21",
  },
]
