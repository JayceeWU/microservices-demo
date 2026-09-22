import Workspace from '../workspace';
export default async function SectionPage({ params }: { params: Promise<{ section: string }> }) {
  const { section } = await params;
  return <Workspace section={section} />;
}
