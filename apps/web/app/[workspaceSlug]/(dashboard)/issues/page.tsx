"use client";

import { IssuesPage } from "@metanicator/views/issues/components";
import { ErrorBoundary } from "@metanicator/ui/components/common/error-boundary";

export default function Page() {
  return (
    <ErrorBoundary>
      <IssuesPage />
    </ErrorBoundary>
  );
}
