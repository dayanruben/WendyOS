"use client";
import { useState } from "react";
import { Loader2, LockKeyhole, Upload, ArrowUpRight } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import type { Connection, Certificate } from "@/lib/client";
import { validateConnection } from "@/lib/client";
const empty: Certificate = {
  pemCertificate: "",
  pemCertificateChain: "",
  pemPrivateKey: "",
  organizationId: 0,
};
export default function ConnectDevice({
  open,
  onClose,
  onConnect,
}: {
  open: boolean;
  onClose: () => void;
  onConnect: (value: Connection) => Promise<void>;
}) {
  const [name, setName] = useState("");
  const [relay, setRelay] = useState("ws://127.0.0.1:8788/tunnel");
  const [cert, setCert] = useState<Certificate>({ ...empty });
  const [assetId, setAssetId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [files, setFiles] = useState<Record<string, string>>({});
  const close = () => {
    if (busy) return;
    setCert({ ...empty });
    setFiles({});
    setError("");
    onClose();
  };
  const read = async (key: keyof Certificate, file?: File) => {
    if (!file) return;
    try {
      if (file.size > 1024 * 1024)
        throw new Error("Certificate files must be smaller than 1 MB.");
      setCert((c) => ({ ...c, [key]: "" }));
      const text = await file.text();
      setCert((c) => ({ ...c, [key]: text }));
      setFiles((f) => ({ ...f, [key]: file.name }));
      setError("");
    } catch (e) {
      setError(String(e));
    }
  };
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    try {
      const c = {
        name: name.trim(),
        relay: relay.trim(),
        certificate: cert,
        assetId: Number(assetId),
      };
      validateConnection(c);
      setBusy(true);
      await onConnect(c);
      setCert({ ...empty });
      setFiles({});
      setName("");
      setAssetId("");
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!value) close();
      }}
    >
      <DialogContent className="connection-dialog">
        <DialogHeader>
          <div className="dialog-icon">
            <LockKeyhole size={24} />
          </div>
          <DialogTitle className="text-2xl">Connect your device</DialogTitle>
          <DialogDescription>
            Connect through a WebSocket relay with your Wendy operator
            certificate.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="form-stack">
          <div className="form-grid">
            <div>
              <Label htmlFor="device-name">Device name</Label>
              <Input
                id="device-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Workshop Jetson"
                required
                maxLength={60}
              />
            </div>
            <div>
              <Label htmlFor="asset-id">Device asset ID</Label>
              <Input
                id="asset-id"
                value={assetId}
                onChange={(e) => setAssetId(e.target.value)}
                placeholder="42"
                type="number"
                min="1"
                required
              />
            </div>
          </div>
          <div>
            <Label htmlFor="relay-url">Relay URL</Label>
            <Input
              id="relay-url"
              type="url"
              value={relay}
              onChange={(e) => setRelay(e.target.value)}
              required
            />
            <p className="field-note">
              Use your local relay or a secure wss:// endpoint.
            </p>
          </div>
          <div>
            <Label htmlFor="org-id">Organization ID</Label>
            <Input
              id="org-id"
              value={cert.organizationId || ""}
              onChange={(e) =>
                setCert((c) => ({
                  ...c,
                  organizationId: Number(e.target.value),
                }))
              }
              type="number"
              min="1"
              required
              placeholder="Your Wendy organization ID"
            />
          </div>
          <Tabs defaultValue="files">
            <TabsList className="w-full">
              <TabsTrigger value="files" className="flex-1">
                Certificate files
              </TabsTrigger>
              <TabsTrigger value="paste" className="flex-1">
                Paste PEM
              </TabsTrigger>
            </TabsList>
            {(["files", "paste"] as const).map((mode) => (
              <TabsContent
                value={mode}
                key={mode}
                className="certificate-fields"
              >
                {(
                  [
                    {
                      key: "pemCertificate",
                      label: "Client certificate",
                      hint: ".pem / .crt",
                    },
                    {
                      key: "pemCertificateChain",
                      label: "CA certificate chain",
                      hint: ".pem / .crt",
                    },
                    {
                      key: "pemPrivateKey",
                      label: "Private key",
                      hint: ".pem / .key",
                    },
                  ] as const
                ).map((f) => (
                  <div key={f.key}>
                    <Label htmlFor={mode + f.key}>{f.label}</Label>
                    {mode === "files" ? (
                      <label className="file-picker" htmlFor={mode + f.key}>
                        <Upload size={16} />
                        <span>{files[f.key] || "Choose file"}</span>
                        <small>{f.hint}</small>
                        <input
                          id={mode + f.key}
                          type="file"
                          accept=".pem,.crt,.key,.cert"
                          onChange={(e) =>
                            void read(f.key, e.target.files?.[0])
                          }
                        />
                      </label>
                    ) : (
                      <Textarea
                        id={mode + f.key}
                        value={String(cert[f.key])}
                        onChange={(e) =>
                          setCert((c) => ({ ...c, [f.key]: e.target.value }))
                        }
                        placeholder={
                          "-----BEGIN " +
                          (f.key === "pemPrivateKey"
                            ? "PRIVATE KEY"
                            : "CERTIFICATE") +
                          "-----"
                        }
                        className="pem-input"
                        spellCheck={false}
                      />
                    )}
                  </div>
                ))}
              </TabsContent>
            ))}
          </Tabs>
          <div className="privacy-note">
            <LockKeyhole size={16} />
            <p>
              Keys stay in this browser session. They are never saved to this
              website or sent to the relay.
            </p>
          </div>
          {error && (
            <div className="inline-error" role="alert">
              {error}
            </div>
          )}
          <div className="dialog-actions">
            <Button
              type="button"
              variant="ghost"
              onClick={close}
              disabled={busy}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={busy}>
              {busy ? (
                <>
                  <Loader2 className="animate-spin" />
                  Connecting…
                </>
              ) : (
                <>
                  Connect device
                  <ArrowUpRight />
                </>
              )}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
