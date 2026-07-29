import { Navigate, Route, Routes } from "react-router-dom";
import { Spin } from "antd";
import { lazy, Suspense, type ReactNode } from "react";
import { useAuth } from "./auth";
import { AppLayout } from "./components/AppLayout";
import { LoginPage } from "./pages/LoginPage";

const DashboardPage = lazy(() =>
  import("./pages/DashboardPage").then((module) => ({
    default: module.DashboardPage,
  })),
);
const SubscriptionsPage = lazy(() =>
  import("./pages/SubscriptionsPage").then((module) => ({
    default: module.SubscriptionsPage,
  })),
);
const ItemsPage = lazy(() =>
  import("./pages/ItemsPage").then((module) => ({
    default: module.ItemsPage,
  })),
);
const SettingsPage = lazy(() =>
  import("./pages/SettingsPage").then((module) => ({
    default: module.SettingsPage,
  })),
);

export function Application() {
  const { session, loading } = useAuth();
  if (loading) {
    return (
      <div className="app-loading">
        <Spin size="large" />
      </div>
    );
  }
  return (
    <Routes>
      <Route
        path="/login"
        element={session ? <Navigate to="/" replace /> : <LoginPage />}
      />
      <Route
        element={session ? <AppLayout /> : <Navigate to="/login" replace />}
      >
        <Route index element={<LazyPage component={<DashboardPage />} />} />
        <Route
          path="subscriptions"
          element={<LazyPage component={<SubscriptionsPage />} />}
        />
        <Route path="items" element={<LazyPage component={<ItemsPage />} />} />
        <Route
          path="settings"
          element={<LazyPage component={<SettingsPage />} />}
        />
      </Route>
      <Route path="*" element={<Navigate to={session ? "/" : "/login"} replace />} />
    </Routes>
  );
}

function LazyPage({ component }: { component: ReactNode }) {
  return (
    <Suspense
      fallback={
        <div className="page-loading">
          <Spin size="large" />
        </div>
      }
    >
      {component}
    </Suspense>
  );
}
