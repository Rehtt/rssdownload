import { LockOutlined, RadarChartOutlined, UserOutlined } from "@ant-design/icons";
import { Alert, Button, Card, Form, Input, Typography } from "antd";
import { useEffect, useState } from "react";
import { api } from "../api";
import { useAuth } from "../auth";

interface LoginValues {
  username: string;
  password: string;
}

const oidcErrors: Record<string, string> = {
  oidc_disabled: "OIDC 登录未启用，请使用密码登录",
  oidc_rate_limited: "登录尝试过于频繁，请稍后再试",
  oidc_invalid_state: "登录请求已失效，请重新发起 OIDC 登录",
  oidc_access_denied: "仅允许邮箱白名单中的已验证账号登录",
  oidc_unavailable: "身份服务暂时不可用，请稍后重试或使用密码登录",
  oidc_failed: "OIDC 登录失败，请重试或使用密码登录",
};

export function LoginPage() {
  const { login } = useAuth();
  const [error, setError] = useState(() => {
    const code = new URLSearchParams(window.location.search).get("oidc_error");
    return code
      ? Object.hasOwn(oidcErrors, code) ? oidcErrors[code] : oidcErrors.oidc_failed
      : "";
  });
  const [submitting, setSubmitting] = useState(false);
  const [methods, setMethods] = useState({ password: true, oidc: false });

  useEffect(() => {
    let active = true;
    api<typeof methods>("/auth/methods")
      .then((result) => {
        if (active) setMethods(result);
      })
      .catch(() => {
        // Keep password login available if loading methods fails.
      });
    return () => {
      active = false;
    };
  }, []);

  const submit = async (values: LoginValues) => {
    setSubmitting(true);
    setError("");
    try {
      await login(values.username, values.password);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "登录失败");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <main className="login-page">
      <div className="login-orb login-orb-one" />
      <div className="login-orb login-orb-two" />
      <section className="login-intro">
        <span className="eyebrow">
          <RadarChartOutlined /> SELF-HOSTED AUTOMATION
        </span>
        <Typography.Title>
          让每一次 RSS 更新
          <br />
          都准确抵达下载队列
        </Typography.Title>
        <Typography.Paragraph>
          聚合 Mikan 订阅、追踪 aria2 任务，在一个克制而清晰的界面里掌握全部下载状态。
        </Typography.Paragraph>
      </section>
      <Card className="login-card" variant="borderless">
        <div className="login-card-title">
          <span className="brand-mark large">R</span>
          <div>
            <Typography.Title level={3}>欢迎回来</Typography.Title>
            <Typography.Text type="secondary">登录管理控制台</Typography.Text>
          </div>
        </div>
        {error && <Alert type="error" showIcon message={error} />}
        {methods.password && (
          <Form<LoginValues>
            layout="vertical"
            requiredMark={false}
            initialValues={{ username: "admin" }}
            onFinish={(values) => void submit(values)}
          >
            <Form.Item
              name="username"
              label="管理员账号"
              rules={[{ required: true, message: "请输入管理员账号" }]}
            >
              <Input size="large" prefix={<UserOutlined />} autoComplete="username" />
            </Form.Item>
            <Form.Item
              name="password"
              label="密码"
              rules={[{ required: true, message: "请输入密码" }]}
            >
              <Input.Password
                size="large"
                prefix={<LockOutlined />}
                autoComplete="current-password"
              />
            </Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              size="large"
              block
              loading={submitting}
            >
              登录
            </Button>
          </Form>
        )}
        {methods.oidc && (
          <Button size="large" block href="/api/v1/auth/oidc/login">
            OIDC 登录
          </Button>
        )}
        <Typography.Text type="secondary" className="login-hint">
          管理员由首次启动时的环境变量创建
        </Typography.Text>
      </Card>
    </main>
  );
}
