'use client';
import { useMutation, useQuery } from '@tanstack/react-query';
import { api, type Studio } from '@dancehub/api-client';
import { useRouter } from 'next/navigation';
import { Empty, ErrorBox, Page } from '../workspace-shared';

export default function Home() {
  const router = useRouter();
  const studios = useQuery({ queryKey: ['studios'], queryFn: () => api.studios() });
  const support = useMutation({
    mutationFn: (studioId: string) => api.createStudioConversation(studioId),
    onSuccess: (value) => {
      if (value.id) router.push(`/chat?conversation=${value.id}`);
    },
  });
  return (
    <>
      <section className="hero">
        <div>
          <p className="eyebrow">Dance · Connect · Belong</p>
          <h1>Find your next dance class.</h1>
          <p className="lede">Book Bay Area studios, classes and rooms with one account.</p>
          <a className="dh-button dh-button--primary" href="/classes">
            Explore classes
          </a>
        </div>
        <div className="orb">
          <span>BA</span>
          <small>Dance starts here</small>
        </div>
      </section>
      <Page title="Studios" subtitle="Across the Bay">
        <ErrorBox error={studios.error} />
        <div className="grid">
          {studios.data?.studios.map((studio: Studio) => (
            <article className="card studio" key={studio.id}>
              <div className="art">{studio.name?.slice(0, 2).toUpperCase()}</div>
              <p className="city">
                {studio.city}, {studio.state}
              </p>
              <h3>{studio.name}</h3>
              <p>{studio.description}</p>
              <div className="card-actions dh-actions">
                <button
                  className="dh-button"
                  type="button"
                  onClick={() => studio.id && support.mutate(studio.id)}
                >
                  Contact studio
                </button>
                <a className="dh-button dh-button--ghost" href={`/chat?studio_id=${studio.id}`}>
                  Enter studio groups
                </a>
              </div>
            </article>
          ))}
        </div>
        {studios.data && !studios.data.studios.length && <Empty name="studios" />}
      </Page>
    </>
  );
}
