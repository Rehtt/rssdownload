import {
  CloudDownloadOutlined,
  DashboardOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  SettingOutlined,
  UnorderedListOutlined,
} from "@ant-design/icons";
import { Avatar, Button, Layout, Menu, Space, Tooltip, Typography } from "antd";
import { useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import { useAuth } from "../auth";

const { Header, Sider, Content } = Layout;

const menuItems = [
  { key: "/", icon: <DashboardOutlined />, label: "概览" },
  {
    key: "/subscriptions",
    icon: <UnorderedListOutlined />,
    label: "订阅管理",
  },
  { key: "/items", icon: <CloudDownloadOutlined />, label: "条目与下载" },
  { key: "/settings", icon: <SettingOutlined />, label: "设置" },
];

export function AppLayout() {
  const [collapsed, setCollapsed] = useState(false);
  const location = useLocation();
  const navigate = useNavigate();
  const { session, logout } = useAuth();
  const selected =
    menuItems.find(
      (item) =>
        item.key === location.pathname ||
        (item.key !== "/" && location.pathname.startsWith(item.key)),
    )?.key ?? "/";

  return (
    <Layout className="app-shell">
      <Sider
        className="app-sider"
        width={232}
        collapsedWidth={72}
        collapsed={collapsed}
        trigger={null}
        breakpoint="lg"
        onBreakpoint={setCollapsed}
      >
        <button
          type="button"
          className="brand"
          onClick={() => navigate("/")}
          aria-label="返回概览"
        >
          <span className="brand-mark">R</span>
          {!collapsed && (
            <span className="brand-copy">
              <strong>RSS Download</strong>
              <small>Mikan × aria2</small>
            </span>
          )}
        </button>
        <Menu
          mode="inline"
          selectedKeys={[selected]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
          className="app-menu"
        />
        <div className="sider-footer">
          <Button
            type="text"
            block
            icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
            onClick={() => setCollapsed((value) => !value)}
          >
            {!collapsed && "收起导航"}
          </Button>
        </div>
      </Sider>
      <Layout>
        <Header className="app-header">
          <Typography.Text className="header-context">
            自动订阅与下载控制台
          </Typography.Text>
          <Space size="middle">
            <Space size={8}>
              <Avatar size="small">{session?.username.slice(0, 1).toUpperCase()}</Avatar>
              <Typography.Text className="header-username">
                {session?.username}
              </Typography.Text>
            </Space>
            <Tooltip title="退出登录">
              <Button
                type="text"
                icon={<LogoutOutlined />}
                aria-label="退出登录"
                onClick={() => void logout()}
              />
            </Tooltip>
          </Space>
        </Header>
        <Content className="app-content">
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}

