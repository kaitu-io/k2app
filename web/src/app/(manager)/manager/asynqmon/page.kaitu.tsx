"use client";

export const dynamic = "force-dynamic";

export default function AsynqmonPage() {
  // Use relative path - goes through Next.js rewrites to backend
  // HttpOnly Cookie is automatically sent with the request
  // Backend route: /app/asynqmon (follows admin API convention: /app/*)
  const iframeSrc = "/app/asynqmon";

  return (
    <div className="h-[calc(100vh-4rem)]">
      <iframe
        src={iframeSrc}
        className="w-full h-full border-0"
        title="Asynqmon - Task Queue Monitor"
      />
    </div>
  );
}
