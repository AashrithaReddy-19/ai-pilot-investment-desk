const PATHS: Record<string, string> = {
  workspace: 'M4 5h16M4 12h16M4 19h10',
  optimize: 'M5 19V9m7 10V5m7 14v-7',
  saved: 'M6 4h9l3 3v13H6zM9 4v5h6V4M9 20v-6h6v6',
  observe: 'M3 12h4l3-7 4 14 3-7h4',
  interviews: 'M5 5h14v10H9l-4 4zM9 9h6',
  checks: 'M5 12l4 4 10-10',
  compare: 'M4 19h6V8H4zM14 19h6V5h-6z',
  about: 'M12 8v.01M11 12h1v5h1M12 3a9 9 0 100 18 9 9 0 000-18z',
  sun: 'M12 4V2m0 20v-2m8-8h2M2 12h2m13.7-5.7 1.4-1.4M4.9 19.1l1.4-1.4m0-11.4L4.9 4.9m14.2 14.2-1.4-1.4M12 8a4 4 0 100 8 4 4 0 000-8z',
  moon: 'M20 14.5A8 8 0 019.5 4a8 8 0 1010.5 10.5z',
  menu: 'M4 7h16M4 12h16M4 17h16',
  collapse: 'M15 6l-6 6 6 6',
  expand: 'M9 6l6 6-6 6',
  close: 'M6 6l12 12M18 6L6 18',
  plus: 'M12 5v14M5 12h14',
  download: 'M12 4v11m0 0l-4-4m4 4l4-4M5 20h14',
  trash: 'M5 7h14M10 7V4h4v3M7 7l1 13h8l1-13',
  edit: 'M4 20h4l11-11-4-4L4 16zM13 7l4 4',
  home: 'M4 11l8-7 8 7M6 9.5V20h12V9.5M10 20v-6h4v6',
  dashboard: 'M4 4h7v9H4zM13 4h7v5h-7zM13 11h7v9h-7zM4 15h7v5H4z',
  baseline: 'M5 20V4h10l4 4v12zM15 4v4h4M8 12h8M8 16h5',
  proposals: 'M4 6h16M4 12h16M4 18h10M18 16v4M16 18h4',
  forecast: 'M4 19h16M5 15l4-4 3 3 7-7M15 7h4v4',
  analysis: 'M11 4a7 7 0 100 14 7 7 0 000-14zM16 16l4 4M8 11h6M11 8v6',
  reports: 'M7 3h7l5 5v13H7zM14 3v5h5M10 13h6M10 17h6',
  settings: 'M12 9a3 3 0 100 6 3 3 0 000-6zM19 12l2-1-1-3-2 .2-1.4-1.4.2-2-3-1-1 2h-2l-1-2-3 1 .2 2L5.6 7.2 3.6 7 3 10l2 1v2l-2 1 1 3 2-.2 1.4 1.4-.2 2 3 1 1-2h2l1 2 3-1-.2-2 1.4-1.4 2 .2 1-3-2-1z',
  upload: 'M12 16V4m0 0l-4 4m4-4l4 4M5 20h14',
  file: 'M6 3h8l4 4v14H6zM14 3v4h4',
  chevron: 'M6 9l6 6 6-6',
  user: 'M12 4a4 4 0 100 8 4 4 0 000-8zM4 20a8 8 0 0116 0',
  more: 'M5 12h.01M12 12h.01M19 12h.01',
  refresh: 'M20 11a8 8 0 10-2.3 5.7M20 4v7h-7',
  alert: 'M12 4l9 16H3zM12 10v4M12 17v.01',
  tools: 'M14 6l4 4M4 20l7-7M13 3l8 8-4 4-8-8z',
}

export function Icon({ name }: { name: string }) {
  return (
    <svg className="ico" viewBox="0 0 24 24" aria-hidden focusable="false">
      <path d={PATHS[name] ?? PATHS.about} />
    </svg>
  )
}
