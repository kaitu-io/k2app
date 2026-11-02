import { redirect } from "next/navigation";

// 节点部署已并入节点管理；保留此路径以兼容旧书签。
export default function CloudPage() {
  redirect("/manager/nodes");
}
