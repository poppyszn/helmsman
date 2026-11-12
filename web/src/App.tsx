import { Navigate, Route, Routes } from "react-router-dom";
import { useMe } from "@/lib/api";
import Layout from "@/components/Layout";
import Login from "@/pages/Login";
import Apps from "@/pages/Apps";
import AppDetail from "@/pages/AppDetail";
import Users from "@/pages/admin/Users";
import Bindings from "@/pages/admin/Bindings";
import Audit from "@/pages/admin/Audit";
import Catalog from "@/pages/admin/Catalog";
import Agents from "@/pages/admin/Agents";
import Approvals from "@/pages/Approvals";
import GitHub from "@/pages/admin/GitHub";
import Integrations from "@/pages/admin/Integrations";
import Notifications from "@/pages/admin/Notifications";
import Doctor from "@/pages/admin/Doctor";
import NoAccess from "@/pages/NoAccess";

function Protected() {
  const { data, isPending, error } = useMe();
  if (isPending) return <p className="p-6 text-sm text-muted">Loading</p>;
  if (error || !data) return <Navigate to="/login" replace />;
  if (!data.has_access) return <NoAccess me={data} />;
  return <Layout />;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route element={<Protected />}>
        <Route path="/" element={<Apps />} />
        <Route path="/apps/:appId" element={<AppDetail />} />
        <Route path="/admin/users" element={<Users />} />
        <Route path="/admin/bindings" element={<Bindings />} />
        <Route path="/admin/catalog" element={<Catalog />} />
        <Route path="/admin/agents" element={<Agents />} />
        <Route path="/approvals" element={<Approvals />} />
        <Route path="/admin/github" element={<GitHub />} />
        <Route path="/admin/integrations" element={<Integrations />} />
        <Route path="/admin/notifications" element={<Notifications />} />
        <Route path="/admin/doctor" element={<Doctor />} />
        <Route path="/admin/audit" element={<Audit />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
