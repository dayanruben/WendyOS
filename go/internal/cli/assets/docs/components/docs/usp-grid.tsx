type USP = {
  flag: string;
  title: string;
  description: string;
};

const usps: USP[] = [
  {
    flag: '--deploy',
    title: 'USB-C Deployment',
    description:
      'Plug in your Jetson or Pi and deploy in seconds. No SSH, no network config, no ceremony.',
  },
  {
    flag: '--debug',
    title: 'Remote Debugging',
    description:
      'Full VSCode debugger with breakpoints over USB or the internet — on any device, anywhere.',
  },
  {
    flag: '--observe',
    title: 'Observability',
    description:
      'Real-time logs, metrics, and distributed traces from every device in your fleet.',
  },
  {
    flag: '--ros2',
    title: 'ROS2 Support',
    description:
      'Run ROS2 nodes natively alongside WendyOS apps on the same hardware.',
  },
  {
    flag: '--ota',
    title: 'Atomic OTA Updates',
    description:
      'Push firmware to one device or ten thousand. Automatic rollback on failure.',
  },
  {
    flag: '--editor',
    title: 'VSCode & Cursor',
    description:
      'Deploy and debug without leaving your editor. Extensions for VSCode, Cursor, and Windsurf.',
  },
  {
    flag: '--open-source',
    title: 'Apache 2.0',
    description:
      'No black boxes, no lock-in. The full OS and toolchain are open source and auditable.',
  },
  {
    flag: '--hardware',
    title: 'NVIDIA & Raspberry Pi',
    description:
      'First-class support for Jetson Orin, AGX Thor, Raspberry Pi 5, and Pi Zero 2W.',
  },
];

export function USPGrid() {
  return (
    <div className="not-prose my-8 grid gap-px bg-fd-border sm:grid-cols-2 lg:grid-cols-4">
      {usps.map((usp) => (
        <div
          key={usp.flag}
          className="flex flex-col gap-3 bg-fd-card p-5 transition-colors hover:bg-fd-accent"
        >
          <span className="font-mono text-[11px] font-medium tracking-wide text-fd-primary">
            {usp.flag}
          </span>
          <h3 className="text-sm font-semibold text-fd-card-foreground">{usp.title}</h3>
          <p className="text-xs leading-relaxed text-fd-muted-foreground">{usp.description}</p>
        </div>
      ))}
    </div>
  );
}
