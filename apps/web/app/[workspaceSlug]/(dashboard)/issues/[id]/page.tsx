"use client";

import { use } from "react";
import { IssueDetailRoute } from "@metanicator/views/issues/components";
import { ErrorBoundary } from "@metanicator/ui/components/common/error-boundary";

export default function IssueDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return (
    <ErrorBoundary resetKeys={[id]}>
      <IssueDetailRoute routeId={id} />
    </ErrorBoundary>
  );
}
