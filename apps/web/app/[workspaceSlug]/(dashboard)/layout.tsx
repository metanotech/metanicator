"use client";

import { DashboardLayout } from "@metanicator/views/layout";
import { MetanicatorIcon } from "@metanicator/ui/components/common/metanicator-icon";
import { SearchCommand, SearchTrigger } from "@metanicator/views/search";
import { FloatingChat } from "@metanicator/views/chat";
import { WebNotificationBridge } from "@/components/web-notification-bridge";

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <DashboardLayout
      loadingIndicator={<MetanicatorIcon className="size-6" />}
      searchSlot={<SearchTrigger />}
      extra={
        <>
          <SearchCommand />
          <WebNotificationBridge />
          <FloatingChat />
        </>
      }
    >
      {children}
    </DashboardLayout>
  );
}
