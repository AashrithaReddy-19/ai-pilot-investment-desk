const PATHS: Record<string, string> = {
  workspace: 'M4 5h16M4 12h16M4 19h10',
  optimize: 'M5 19V9m7 10V5m7 14v-7',
  saved: 'M6 4h9l3 3v13H6zM9 4v5h6V4M9 20v-6h6v6',
  observe: 'M3 12h4l3-7 4 14 3-7h4',
  interviews: 'M5 5h14v10H9l-4 4zM9 9h6',
  checks: 'M5 12l4 4 10-10',
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
}

export function Icon({ name }: { name: string }) {
  return (
    <svg className="ico" viewBox="0 0 24 24" aria-hidden focusable="false">
      <path d={PATHS[name] ?? PATHS.about} />
    </svg>
  )
}
