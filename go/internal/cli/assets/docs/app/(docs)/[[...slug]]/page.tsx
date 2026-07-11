import { getMDXComponents } from '@/components/mdx';
import { source } from '@/lib/source';
import { ogImage, withBasePath } from '@/lib/shared';
import { DocsBody, DocsDescription, DocsPage, DocsTitle } from 'fumadocs-ui/layouts/docs/page';
import { createRelativeLink } from 'fumadocs-ui/mdx';
import type { Metadata } from 'next';
import { notFound, permanentRedirect } from 'next/navigation';

type PageParams = {
  slug?: string[];
};

type PageProps = {
  params: Promise<PageParams>;
};

function isLegacyJetsonRoute(slug?: string[]) {
  return slug?.join('/') === 'installation/wendyos-nvidia-jetson';
}

export default async function Page(props: PageProps) {
  const params = await props.params;
  if (isLegacyJetsonRoute(params.slug)) {
    permanentRedirect(withBasePath('/installation/wendyos-nvidia-jetson-orin-nano/'));
  }

  const page = source.getPage(params.slug);
  if (!page) notFound();

  const MDX = page.data.body;

  return (
    <DocsPage toc={page.data.toc} full={page.data.full}>
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsDescription>{page.data.description}</DocsDescription>
      <DocsBody>
        <MDX
          components={getMDXComponents({
            a: createRelativeLink(source, page),
          })}
        />
      </DocsBody>
    </DocsPage>
  );
}

export function generateStaticParams() {
  return source.generateParams();
}

export async function generateMetadata(props: PageProps): Promise<Metadata> {
  const params = await props.params;
  if (isLegacyJetsonRoute(params.slug)) {
    return {
      title: 'NVIDIA Jetson Orin Nano',
      description: 'Install WendyOS on an NVIDIA Jetson Orin Nano.',
    };
  }

  const page = source.getPage(params.slug);
  if (!page) notFound();

  return {
    title: page.data.title,
    description: page.data.description,
    openGraph: {
      images: ogImage,
    },
    twitter: {
      card: 'summary_large_image',
      images: ogImage,
    },
  };
}
