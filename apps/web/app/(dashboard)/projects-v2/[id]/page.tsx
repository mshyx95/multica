"use client";

import { use } from "react";
import { ProjectV2Detail } from "@multica/views/projects-v2";

export default function ProjectV2DetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <ProjectV2Detail projectId={id} />;
}
