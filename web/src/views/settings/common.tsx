import type { ComponentChildren } from "preact";

export function PageHead({ title, intro, actions }: { title: string; intro?: string; actions?: ComponentChildren }) {
  return (
    <header class="page-head">
      <div>
        <h1 class="page-title">{title}</h1>
        {intro ? <p class="page-intro">{intro}</p> : null}
      </div>
      {actions ? <div class="page-actions">{actions}</div> : null}
    </header>
  );
}

export function Section({ title, children, id }: { title: string; children: ComponentChildren; id?: string }) {
  return (
    <section class="sect" aria-labelledby={id}>
      <h2 class="sect-title" id={id}>{title}</h2>
      {children}
    </section>
  );
}
