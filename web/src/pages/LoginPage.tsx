import { LockOutlined, RadarChartOutlined, UserOutlined } from "@ant-design/icons";
import { Alert, Button, Card, Form, Input, Typography } from "antd";
import { useState } from "react";
import { useAuth } from "../auth";

interface LoginValues {
  username: string;
  password: string;
}

export function LoginPage() {
  const { login } = useAuth();
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

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
        <Typography.Text type="secondary" className="login-hint">
          管理员由首次启动时的环境变量创建
        </Typography.Text>
      </Card>
    </main>
  );
}
