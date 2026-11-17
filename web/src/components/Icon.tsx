const paths = {
  home: "m3 10 9-7 9 7v10a1 1 0 0 1-1 1h-5v-7H9v7H4a1 1 0 0 1-1-1Z",
  album: "M4 4h16v16H4z M4 16l5-5 4 4 3-3 4 4 M15 8h.01",
  mine: "M20 21v-2a6 6 0 0 0-6-6h-4a6 6 0 0 0-6 6v2 M12 9a4 4 0 1 0 0-8 4 4 0 0 0 0 8",
  chart: "M4 3v18h17 M8 16v-4 M13 16V7 M18 16V4",
  manage: "M3 7h7l2 3h9v10H3z M3 7V4h7l2 3h9v3",
  upload: "M12 16V3 M7 8l5-5 5 5 M4 16v5h16v-5",
  settings:
    "m9 3-1 3-3 1-2 3 2 2-1 4 3 2 3-1 3 2 3-2 1-3 3-1v-4l-3-2-1-3z M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8",
  search: "M10.5 3a7.5 7.5 0 1 0 0 15 7.5 7.5 0 0 0 0-15 M16 16l5 5",
  heart:
    "M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.7l-1.1-1.1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8",
  star: "m12 2 3 6 7 1-5 5 1 7-6-3-6 3 1-7-5-5 7-1z",
  back: "m14 5-7 7 7 7 M7 12h14",
  close: "m6 6 12 12 M6 18 18 6",
  refresh: "M20 7V2l-3 3a9 9 0 1 0 3 11 M20 7h-5",
  trash: "M3 6h18 M9 6V3h6v3 M5 6l1 15h12l1-15 M10 10v7 M14 10v7",
  play: "m8 4 13 8-13 8z",
  task: "M8 3H4v18h16V3h-4 M8 2h8v4H8z M8 11l2 2 6-5 M8 17h8",
  server: "M3 3h18v7H3z M3 14h18v7H3z M7 6.5h.01 M7 17.5h.01",
  menu: "M4 6h16 M4 12h16 M4 18h16",
  arrow: "M5 12h14 M13 6l6 6-6 6",
  image: "M3 3h18v18H3z M3 17l6-6 5 5 3-3 4 4 M15 7h.01",
  sun: "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8 M12 1v3 M12 20v3 M1 12h3 M20 12h3 M4 4l2 2 M18 18l2 2 M4 20l2-2 M18 6l2-2",
} as const;
export type IconName = keyof typeof paths;
export function Icon({ name, size = 22 }: { name: IconName; size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={paths[name]} />
    </svg>
  );
}
