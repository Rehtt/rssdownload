import {
  EditOutlined,
  InboxOutlined,
  MoreOutlined,
  PlusOutlined,
  RedoOutlined,
  SyncOutlined,
} from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  App,
  Button,
  Dropdown,
  Form,
  Input,
  Modal,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { useMemo, useState } from "react";
import { api } from "../api";
import { PageHeader } from "../components/PageHeader";
import { formatDate } from "../format";
import type { Subscription } from "../types";

interface SubscriptionForm {
  name: string;
  url: string;
  downloadDir: string;
  downloadExisting: boolean;
}

export function SubscriptionsPage() {
  const { message, modal } = App.useApp();
  const queryClient = useQueryClient();
  const [includeArchived, setIncludeArchived] = useState(false);
  const [editing, setEditing] = useState<Subscription | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [form] = Form.useForm<SubscriptionForm>();

  const subscriptions = useQuery({
    queryKey: ["subscriptions", includeArchived],
    queryFn: () =>
      api<Subscription[]>(
        `/subscriptions${includeArchived ? "?includeArchived=true" : ""}`,
      ),
    refetchInterval: 10_000,
  });

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["subscriptions"] }),
      queryClient.invalidateQueries({ queryKey: ["dashboard"] }),
    ]);
  };

  const create = useMutation({
    mutationFn: (values: SubscriptionForm) =>
      api<Subscription>("/subscriptions", {
        method: "POST",
        body: JSON.stringify(values),
      }),
    onSuccess: async () => {
      message.success("订阅已添加，首次同步正在后台执行");
      setFormOpen(false);
      form.resetFields();
      await refresh();
    },
    onError: (error) => message.error(error.message),
  });

  const update = useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id: number;
      values: Partial<SubscriptionForm> & { enabled?: boolean };
    }) =>
      api<Subscription>(`/subscriptions/${id}`, {
        method: "PATCH",
        body: JSON.stringify(values),
      }),
    onSuccess: async () => {
      message.success("订阅已更新");
      setFormOpen(false);
      setEditing(null);
      form.resetFields();
      await refresh();
    },
    onError: (error) => message.error(error.message),
  });

  const action = useMutation({
    mutationFn: async ({
      id,
      operation,
    }: {
      id: number;
      operation: "sync" | "archive" | "restore";
    }) => {
      if (operation === "archive") {
        return api<void>(`/subscriptions/${id}`, { method: "DELETE" });
      }
      return api<void>(`/subscriptions/${id}/${operation}`, { method: "POST" });
    },
    onSuccess: async (_, variables) => {
      message.success(
        variables.operation === "sync"
          ? "已触发同步"
          : variables.operation === "archive"
            ? "订阅已归档"
            : "订阅已恢复",
      );
      await refresh();
    },
    onError: (error) => message.error(error.message),
  });

  const openCreate = () => {
    setEditing(null);
    form.setFieldsValue({
      name: "",
      url: "",
      downloadDir: "",
      downloadExisting: false,
    });
    setFormOpen(true);
  };

  const openEdit = (subscription: Subscription) => {
    setEditing(subscription);
    form.setFieldsValue({
      name: subscription.name,
      url: "",
      downloadDir: subscription.downloadDir,
      downloadExisting: false,
    });
    setFormOpen(true);
  };

  const archive = (subscription: Subscription) => {
    modal.confirm({
      title: "归档这个订阅？",
      content: "后台将停止轮询，但历史条目、下载记录和 aria2 任务都会保留。",
      okText: "归档",
      okButtonProps: { danger: true },
      cancelText: "取消",
      onOk: () =>
        action.mutateAsync({ id: subscription.id, operation: "archive" }),
    });
  };

  const columns: ColumnsType<Subscription> = useMemo(
    () => [
      {
        title: "订阅",
        key: "name",
        render: (_, record) => (
          <div className="table-primary">
            <Typography.Text strong>{record.name || "等待首次同步"}</Typography.Text>
            <Typography.Text type="secondary" ellipsis={{ tooltip: record.url }}>
              {record.url}
            </Typography.Text>
          </div>
        ),
      },
      {
        title: "状态",
        key: "status",
        width: 132,
        render: (_, record) =>
          record.archivedAt ? (
            <Tag>已归档</Tag>
          ) : record.enabled ? (
            <Tag color="green">轮询中</Tag>
          ) : (
            <Tag color="gold">已停用</Tag>
          ),
      },
      {
        title: "下载目录",
        dataIndex: "downloadDir",
        responsive: ["lg"],
        render: (value: string) => value || <Typography.Text type="secondary">继承全局</Typography.Text>,
      },
      {
        title: "上次检查",
        dataIndex: "lastCheckedAt",
        width: 180,
        responsive: ["md"],
        render: formatDate,
      },
      {
        title: "启用",
        key: "enabled",
        width: 80,
        render: (_, record) => (
          <Switch
            size="small"
            checked={record.enabled && !record.archivedAt}
            disabled={Boolean(record.archivedAt)}
            loading={update.isPending}
            onChange={(enabled) =>
              update.mutate({ id: record.id, values: { enabled } })
            }
          />
        ),
      },
      {
        title: "",
        key: "actions",
        width: 114,
        fixed: "right",
        render: (_, record) => (
          <Space size={4}>
            {!record.archivedAt && (
              <Tooltip title="立即同步">
                <Button
                  type="text"
                  icon={<SyncOutlined spin={action.isPending} />}
                  onClick={() =>
                    action.mutate({ id: record.id, operation: "sync" })
                  }
                />
              </Tooltip>
            )}
            <Dropdown
              trigger={["click"]}
              menu={{
                items: record.archivedAt
                  ? [
                      {
                        key: "restore",
                        icon: <RedoOutlined />,
                        label: "恢复订阅",
                      },
                    ]
                  : [
                      { key: "edit", icon: <EditOutlined />, label: "编辑" },
                      {
                        key: "archive",
                        icon: <InboxOutlined />,
                        label: "归档",
                        danger: true,
                      },
                    ],
                onClick: ({ key }) => {
                  if (key === "edit") openEdit(record);
                  if (key === "archive") archive(record);
                  if (key === "restore") {
                    action.mutate({ id: record.id, operation: "restore" });
                  }
                },
              }}
            >
              <Button type="text" icon={<MoreOutlined />} />
            </Dropdown>
          </Space>
        ),
      },
    ],
    [action, update],
  );

  const submit = (values: SubscriptionForm) => {
    if (editing) {
      update.mutate({
        id: editing.id,
        values: {
          name: values.name,
          url: values.url,
          downloadDir: values.downloadDir,
        },
      });
    } else {
      create.mutate(values);
    }
  };

  return (
    <>
      <PageHeader
        title="订阅管理"
        description="管理 Mikan MyBangumi RSS 地址及每个订阅的下载目录。"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            添加订阅
          </Button>
        }
      />
      {subscriptions.error && (
        <Alert
          type="error"
          showIcon
          message="订阅加载失败"
          description={subscriptions.error.message}
        />
      )}
      <div className="toolbar-row">
        <Space>
          <Switch checked={includeArchived} onChange={setIncludeArchived} />
          <Typography.Text type="secondary">显示已归档订阅</Typography.Text>
        </Space>
      </div>
      <Table
        rowKey="id"
        columns={columns}
        dataSource={subscriptions.data ?? []}
        loading={subscriptions.isLoading}
        pagination={false}
        scroll={{ x: 800 }}
        expandable={{
          expandedRowRender: (record) =>
            record.lastError ? (
              <Alert type="error" showIcon message="最近同步失败" description={record.lastError} />
            ) : (
              <Typography.Text type="secondary">
                下次检查：{formatDate(record.nextCheckAt)}
              </Typography.Text>
            ),
        }}
      />
      <Modal
        open={formOpen}
        title={editing ? "编辑订阅" : "添加 Mikan 订阅"}
        okText={editing ? "保存" : "添加并同步"}
        cancelText="取消"
        confirmLoading={create.isPending || update.isPending}
        onCancel={() => setFormOpen(false)}
        onOk={() => form.submit()}
        destroyOnHidden
      >
        <Form<SubscriptionForm>
          form={form}
          layout="vertical"
          requiredMark={false}
          onFinish={submit}
          initialValues={{ downloadExisting: false }}
        >
          <Form.Item name="name" label="显示名称">
            <Input placeholder="可留空，首次同步后使用 RSS 频道名称" maxLength={120} />
          </Form.Item>
          <Form.Item
            name="url"
            label="RSS 地址"
            extra={
              editing
                ? "留空表示不修改当前地址。"
                : "仅支持 https://mikanani.me/RSS/MyBangumi?token=..."
            }
            rules={
              editing
                ? []
                : [{ required: true, message: "请输入 Mikan MyBangumi RSS 地址" }]
            }
          >
            <Input.Password
              placeholder={editing ? "留空保持原地址" : "https://mikanani.me/RSS/MyBangumi?token=..."}
              autoComplete="off"
            />
          </Form.Item>
          <Form.Item
            name="downloadDir"
            label="aria2 下载目录覆盖"
            extra="留空时使用全局目录；全局也为空时使用 aria2 自身默认目录。"
          >
            <Input placeholder="/downloads/anime" />
          </Form.Item>
          {!editing && (
            <Form.Item
              name="downloadExisting"
              label="首次同步"
              valuePropName="checked"
            >
              <Switch checkedChildren="下载当前条目" unCheckedChildren="仅建立基线" />
            </Form.Item>
          )}
        </Form>
      </Modal>
    </>
  );
}
