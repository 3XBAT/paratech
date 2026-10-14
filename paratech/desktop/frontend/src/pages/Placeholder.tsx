export function Placeholder({ title }: { title: string }) {
  return (
    <section>
      <h1>{title}</h1>
      <p className="muted">Раздел появится в следующих версиях.</p>
    </section>
  );
}
