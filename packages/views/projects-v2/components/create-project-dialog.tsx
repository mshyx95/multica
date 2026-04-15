"use client";

import { useState } from "react";
import { ChevronDown } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";
import type { CreateProjectV2Request } from "@multica/core/types";
import { COPILOT_MODELS, EFFORT_LEVELS } from "../config";

export function CreateProjectDialog({
  onClose,
  onCreate,
}: {
  onClose: () => void;
  onCreate: (data: CreateProjectV2Request) => Promise<void>;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [goals, setGoals] = useState("");
  const [executors, setExecutors] = useState(2);
  const [model, setModel] = useState("");
  const [effort, setEffort] = useState<string>("medium");
  const [creating, setCreating] = useState(false);
  const [modelOpen, setModelOpen] = useState(false);
  const [effortOpen, setEffortOpen] = useState(false);

  const selectedEffort = EFFORT_LEVELS.find((e) => e.id === effort);

  const handleSubmit = async () => {
    if (!name.trim()) return;
    setCreating(true);
    try {
      await onCreate({
        name: name.trim(),
        description: description.trim() || undefined,
        goals: goals.trim() || undefined,
        config: {
          num_executors: executors,
          model: model || undefined,
          effort,
        },
      });
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to create project");
      setCreating(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New Project</DialogTitle>
          <DialogDescription>
            Create a new multi-agent project for your workspace.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 min-w-0">
          <div>
            <Label className="text-xs text-muted-foreground">Name</Label>
            <Input
              autoFocus
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Refactor Auth Module"
              className="mt-1"
              onKeyDown={(e) => e.key === "Enter" && handleSubmit()}
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Description</Label>
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Brief description of the project"
              className="mt-1 min-h-[60px]"
              rows={2}
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Goals</Label>
            <Textarea
              value={goals}
              onChange={(e) => setGoals(e.target.value)}
              placeholder="What should this project achieve?"
              className="mt-1 min-h-[60px]"
              rows={2}
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <Label className="text-xs text-muted-foreground">Executors (1–4)</Label>
              <Input
                type="number"
                min={1}
                max={4}
                value={executors}
                onChange={(e) => setExecutors(Math.min(4, Math.max(1, Number(e.target.value) || 1)))}
                className="mt-1"
              />
            </div>

            <div>
              <Label className="text-xs text-muted-foreground">Effort</Label>
              <Popover open={effortOpen} onOpenChange={setEffortOpen}>
                <PopoverTrigger className="flex w-full items-center justify-between rounded-lg border border-border bg-background px-3 py-2 mt-1 text-left text-sm transition-colors hover:bg-muted">
                  <span className="font-medium">{selectedEffort?.label ?? "Medium"}</span>
                  <ChevronDown className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${effortOpen ? "rotate-180" : ""}`} />
                </PopoverTrigger>
                <PopoverContent align="start" className="w-[var(--anchor-width)] p-1">
                  {EFFORT_LEVELS.map((e) => (
                    <button
                      key={e.id}
                      onClick={() => { setEffort(e.id); setEffortOpen(false); }}
                      className={`flex w-full flex-col items-start rounded-md px-3 py-2 text-left text-sm transition-colors ${
                        effort === e.id ? "bg-accent" : "hover:bg-accent/50"
                      }`}
                    >
                      <span className="font-medium">{e.label}</span>
                      <span className="text-xs text-muted-foreground">{e.description}</span>
                    </button>
                  ))}
                </PopoverContent>
              </Popover>
            </div>
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Model</Label>
            <Popover open={modelOpen} onOpenChange={setModelOpen}>
              <PopoverTrigger className="flex w-full items-center justify-between rounded-lg border border-border bg-background px-3 py-2 mt-1 text-left text-sm transition-colors hover:bg-muted">
                <span className="truncate font-medium">
                  {model
                    ? (COPILOT_MODELS.find((m) => m.id === model)?.name ?? model)
                    : "Default"}
                </span>
                <ChevronDown className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${modelOpen ? "rotate-180" : ""}`} />
              </PopoverTrigger>
              <PopoverContent align="start" className="w-[var(--anchor-width)] p-1 max-h-60 overflow-y-auto">
                <button
                  onClick={() => { setModel(""); setModelOpen(false); }}
                  className={`flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors ${
                    !model ? "bg-accent" : "hover:bg-accent/50"
                  }`}
                >
                  <span className="font-medium">Default</span>
                </button>
                {COPILOT_MODELS.map((m) => (
                  <button
                    key={m.id}
                    onClick={() => { setModel(m.id); setModelOpen(false); }}
                    className={`flex w-full items-center justify-between gap-3 rounded-md px-3 py-2 text-left text-sm transition-colors ${
                      model === m.id ? "bg-accent" : "hover:bg-accent/50"
                    }`}
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <span className="font-medium truncate">{m.name}</span>
                      {m.isDefault && <span className="shrink-0 rounded bg-primary/10 px-1 py-0.5 text-[10px] font-medium text-primary">default</span>}
                    </div>
                    <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{m.cost}</span>
                  </button>
                ))}
              </PopoverContent>
            </Popover>
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={creating || !name.trim()}
          >
            {creating ? "Creating..." : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
