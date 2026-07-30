import {
  ApiOutlined,
  GlobalOutlined,
  KeyOutlined,
  LockOutlined,
  SaveOutlined,
} from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  App,
  Button,
  Card,
  Checkbox,
  Col,
  Divider,
  Form,
  Input,
  InputNumber,
  Row,
  Space,
  Typography,
} from "antd";
import { useEffect } from "react";
import { api } from "../api";
import { PageHeader } from "../components/PageHeader";
import type { AriaVersion, Settings } from "../types";

interface SettingsForm {
  aria2Url: string;
  aria2Secret: string;
  clearAria2Secret: boolean;
  seedTimeMinutes: number | null;
  seedRatio: number | null;
  proxyUrl: string;
  proxyUsername: string;
  proxyPassword: string;
  clearProxyPassword: boolean;
  defaultDownloadDir: string;
  pollIntervalMinutes: number;
}

interface PasswordForm {
  currentPassword: string;
  newPassword: string;
  confirmPassword: string;
}

function proxyUrlError(value: unknown) {
  if (typeof value !== "string" || value.trim() === "") {
    return undefined;
  }
  try {
    const parsed = new URL(value.trim());
    if (
      !["http:", "https:", "socks5:", "socks5h:"].includes(parsed.protocol) ||
      !parsed.hostname
    ) {
      return "请输入有效的 HTTP(S) 或 SOCKS5 代理地址";
    }
    if (parsed.username || parsed.password) {
      return "请在下方认证栏填写代理账号和密码";
    }
    if (
      (parsed.pathname !== "" && parsed.pathname !== "/") ||
      parsed.search ||
      parsed.hash
    ) {
      return "代理地址不能包含路径、查询参数或片段";
    }
  } catch {
    return "请输入有效的 HTTP(S) 或 SOCKS5 代理地址";
  }
  return undefined;
}

export function SettingsPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [settingsForm] = Form.useForm<SettingsForm>();
  const [passwordForm] = Form.useForm<PasswordForm>();
  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => api<Settings>("/settings"),
  });

  useEffect(() => {
    if (settings.data) {
      settingsForm.setFieldsValue({
        aria2Url: settings.data.aria2Url,
        aria2Secret: "",
        clearAria2Secret: false,
        seedTimeMinutes: settings.data.seedTimeMinutes,
        seedRatio: settings.data.seedRatio,
        proxyUrl: settings.data.proxyUrl,
        proxyUsername: settings.data.proxyUsername,
        proxyPassword: "",
        clearProxyPassword: false,
        defaultDownloadDir: settings.data.defaultDownloadDir,
        pollIntervalMinutes: settings.data.pollIntervalMinutes,
      });
    }
  }, [settings.data, settingsForm]);

  const save = useMutation({
    mutationFn: (values: SettingsForm) =>
      api<Settings>("/settings", {
        method: "PUT",
        body: JSON.stringify({
          ...values,
          seedTimeMinutes: values.seedTimeMinutes ?? null,
          seedRatio: values.seedRatio ?? null,
        }),
      }),
    onSuccess: async () => {
      message.success("设置已保存");
      await queryClient.invalidateQueries({ queryKey: ["settings"] });
    },
    onError: (error) => message.error(error.message),
  });

  const test = useMutation({
    mutationFn: (values: SettingsForm) =>
      api<AriaVersion>("/settings/test-aria2", {
        method: "POST",
        body: JSON.stringify(values),
      }),
    onSuccess: (version) =>
      message.success(`连接成功：aria2 ${version.version}`),
    onError: (error) => message.error(error.message),
  });

  const changePassword = useMutation({
    mutationFn: (values: PasswordForm) =>
      api<void>("/auth/password", {
        method: "PUT",
        body: JSON.stringify({
          currentPassword: values.currentPassword,
          newPassword: values.newPassword,
        }),
      }),
    onSuccess: () => {
      message.success("密码已修改，其他登录会话已失效");
      passwordForm.resetFields();
    },
    onError: (error) => message.error(error.message),
  });

  const currentValues = async () => {
    const values = await settingsForm.validateFields();
    return values;
  };

  return (
    <>
      <PageHeader
        title="系统设置"
        description="配置 aria2 JSON-RPC、做种策略、网络代理、下载目录、轮询频率和管理员密码。"
      />
      {settings.error && (
        <Alert
          type="error"
          showIcon
          message="设置加载失败"
          description={settings.error.message}
        />
      )}
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={15}>
          <Card
            title={
              <Space>
                <ApiOutlined />
                下载与网络
              </Space>
            }
            loading={settings.isLoading}
          >
            <Form<SettingsForm>
              form={settingsForm}
              layout="vertical"
              requiredMark={false}
              onFinish={(values) => save.mutate(values)}
            >
              <Form.Item
                name="aria2Url"
                label="JSON-RPC 地址"
                rules={[
                  { required: true, message: "请输入 aria2 JSON-RPC 地址" },
                  { type: "url", message: "请输入有效的 HTTP(S) URL" },
                ]}
              >
                <Input placeholder="http://127.0.0.1:6800/jsonrpc" />
              </Form.Item>
              <Form.Item
                name="aria2Secret"
                label="RPC Secret"
                extra={
                  settings.data?.hasAria2Secret
                    ? "已保存密钥；留空表示保持不变。"
                    : "aria2 未配置密钥时可留空。"
                }
              >
                <Input.Password
                  prefix={<KeyOutlined />}
                  placeholder={
                    settings.data?.hasAria2Secret ? "留空保持当前密钥" : "aria2 RPC secret"
                  }
                  autoComplete="new-password"
                />
              </Form.Item>
              {settings.data?.hasAria2Secret && (
                <Form.Item name="clearAria2Secret" valuePropName="checked">
                  <Checkbox>清除已保存的 RPC Secret</Checkbox>
                </Form.Item>
              )}
              <Divider titlePlacement="start">
                <Space>
                  <GlobalOutlined />
                  网络代理
                </Space>
              </Divider>
              <Form.Item
                name="proxyUrl"
                label="代理地址"
                extra="用于获取 Mikan RSS 和下载种子，不影响 aria2 RPC。留空时使用服务进程的 HTTP_PROXY/HTTPS_PROXY 环境变量。"
                rules={[
                  {
                    validator: (_, value) => {
                      const error = proxyUrlError(value);
                      return error
                        ? Promise.reject(new Error(error))
                        : Promise.resolve();
                    },
                  },
                ]}
              >
                <Input
                  prefix={<GlobalOutlined />}
                  placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:1080"
                />
              </Form.Item>
              <Row gutter={16}>
                <Col xs={24} md={12}>
                  <Form.Item
                    name="proxyUsername"
                    label="代理账号"
                    extra="代理无需认证时留空。"
                  >
                    <Input autoComplete="off" placeholder="可选" />
                  </Form.Item>
                </Col>
                <Col xs={24} md={12}>
                  <Form.Item
                    name="proxyPassword"
                    label="代理密码"
                    extra={
                      settings.data?.hasProxyPassword
                        ? "已保存密码；留空表示保持不变。"
                        : "代理无需认证时留空。"
                    }
                  >
                    <Input.Password
                      placeholder={
                        settings.data?.hasProxyPassword ? "留空保持当前密码" : "可选"
                      }
                      autoComplete="new-password"
                    />
                  </Form.Item>
                </Col>
              </Row>
              {settings.data?.hasProxyPassword && (
                <Form.Item name="clearProxyPassword" valuePropName="checked">
                  <Checkbox>清除已保存的代理密码</Checkbox>
                </Form.Item>
              )}
              <Divider titlePlacement="start">下载与轮询</Divider>
              <Form.Item
                name="defaultDownloadDir"
                label="默认下载目录"
                extra="留空时由 aria2 决定；订阅级目录优先于此设置。"
              >
                <Input placeholder="/downloads/anime" />
              </Form.Item>
              <Row gutter={16}>
                <Col xs={24} md={12}>
                  <Form.Item
                    name="seedTimeMinutes"
                    label="做种时间（分钟）"
                    extra="留空时沿用 aria2 配置；0 表示下载完成后不做种。"
                  >
                    <InputNumber
                      min={0}
                      step={0.1}
                      placeholder="由 aria2 决定"
                      style={{ width: "100%" }}
                    />
                  </Form.Item>
                </Col>
                <Col xs={24} md={12}>
                  <Form.Item
                    name="seedRatio"
                    label="做种比例（分享率）"
                    extra="留空时沿用 aria2 配置；0 表示不按分享率停止，建议不低于 1。"
                  >
                    <InputNumber
                      min={0}
                      step={0.1}
                      placeholder="由 aria2 决定"
                      style={{ width: "100%" }}
                    />
                  </Form.Item>
                </Col>
              </Row>
              <Typography.Paragraph type="secondary">
                同时设置时，达到任一条件即停止做种；仅影响之后提交给 aria2 的任务。
              </Typography.Paragraph>
              <Form.Item
                name="pollIntervalMinutes"
                label="RSS 轮询间隔"
                rules={[{ required: true, message: "请输入轮询间隔" }]}
              >
                <InputNumber min={1} max={1440} addonAfter="分钟" />
              </Form.Item>
              <Space wrap>
                <Button
                  type="primary"
                  htmlType="submit"
                  icon={<SaveOutlined />}
                  loading={save.isPending}
                >
                  保存设置
                </Button>
                <Button
                  icon={<ApiOutlined />}
                  loading={test.isPending}
                  onClick={() => {
                    void currentValues().then((values) => test.mutate(values));
                  }}
                >
                  测试 aria2
                </Button>
              </Space>
            </Form>
          </Card>
        </Col>
        <Col xs={24} xl={9}>
          <Card
            title={
              <Space>
                <LockOutlined />
                管理员密码
              </Space>
            }
          >
            <Typography.Paragraph type="secondary">
              修改后当前浏览器保持登录，其他会话将立即失效。
            </Typography.Paragraph>
            <Form<PasswordForm>
              form={passwordForm}
              layout="vertical"
              requiredMark={false}
              onFinish={(values) => changePassword.mutate(values)}
            >
              <Form.Item
                name="currentPassword"
                label="当前密码"
                rules={[{ required: true, message: "请输入当前密码" }]}
              >
                <Input.Password autoComplete="current-password" />
              </Form.Item>
              <Form.Item
                name="newPassword"
                label="新密码"
                rules={[
                  { required: true, message: "请输入新密码" },
                  { min: 8, message: "密码至少需要 8 个字符" },
                ]}
              >
                <Input.Password autoComplete="new-password" />
              </Form.Item>
              <Form.Item
                name="confirmPassword"
                label="确认新密码"
                dependencies={["newPassword"]}
                rules={[
                  { required: true, message: "请再次输入新密码" },
                  ({ getFieldValue }) => ({
                    validator(_, value) {
                      if (!value || getFieldValue("newPassword") === value) {
                        return Promise.resolve();
                      }
                      return Promise.reject(new Error("两次输入的密码不一致"));
                    },
                  }),
                ]}
              >
                <Input.Password autoComplete="new-password" />
              </Form.Item>
              <Button
                type="primary"
                htmlType="submit"
                loading={changePassword.isPending}
              >
                修改密码
              </Button>
            </Form>
          </Card>
        </Col>
      </Row>
    </>
  );
}
