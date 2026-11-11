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

function Protected() {
  const { data, isPending, error } = useMe();
  if (isPending) return <p className="p-6 text-sm text-muted">Loading</p>;
  if (error || !data) return <Navigate to="/login" replace />;
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
        <Route path="/admin/audit" element={<Audit />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
