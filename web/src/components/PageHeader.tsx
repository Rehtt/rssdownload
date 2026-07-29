import type { ReactNode } from "react";
import { Typography } from "antd";

export function PageHeader({
  title,
  description,
  extra,
}: {
  title: string;
  description: string;
  extra?: ReactNode;
}) {
  return (
    <div className="page-heading">
      <div>
        <Typography.Title level={2}>{title}</Typography.Title>
        <Typography.Paragraph>{description}</Typography.Paragraph>
      </div>
      {extra && <div className="page-heading-extra">{extra}</div>}
    </div>
  );
}

