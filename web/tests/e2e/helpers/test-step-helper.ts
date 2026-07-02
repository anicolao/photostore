import { expect, type Page, type TestInfo } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';

export type Verification = {
  spec: string;
  check: () => Promise<void>;
};

export type StepOptions = {
  description: string;
  verifications: Verification[];
};

type DocStep = {
  title: string;
  image: string;
  specs: string[];
};

export class TestStepHelper {
  private stepCount = 0;
  private steps: DocStep[] = [];
  private title = '';
  private description = '';

  constructor(
    private page: Page,
    private testInfo: TestInfo
  ) {}

  setMetadata(title: string, description: string) {
    this.title = title;
    this.description = description;
  }

  async step(id: string, options: StepOptions) {
    console.log(`\n[E2E step] ${id}: ${options.description}`);
    for (const verification of options.verifications) {
      console.log(`[E2E check] ${verification.spec}`);
      await verification.check();
      console.log(`[E2E pass] ${verification.spec}`);
    }

    const paddedIndex = String(this.stepCount++).padStart(3, '0');
    const filename = `${paddedIndex}-${id.replace(/_/g, '-')}.png`;
    await expect(this.page).toHaveScreenshot(filename.replace(/\.png$/, ''), {
      mask: [this.page.locator('[data-screenshot-mask]')]
    });

    this.steps.push({
      title: options.description,
      image: `./screenshots/${filename}`,
      specs: options.verifications.map((v) => v.spec)
    });
  }

  generateDocs() {
    const docPath = join(dirname(this.testInfo.file), 'README.md');
    mkdirSync(dirname(docPath), { recursive: true });
    const title = this.title || this.testInfo.title;
    let content = `# Test: ${title}\n\n`;
    if (this.description) {
      content += `${this.description}\n\n`;
    }
    for (const step of this.steps) {
      content += `## ${step.title}\n\n`;
      content += `![${step.title}](${step.image})\n\n`;
      content += `**Verifications:**\n`;
      for (const spec of step.specs) {
        content += `- [x] ${spec}\n`;
      }
      content += `\n---\n\n`;
    }
    writeFileSync(docPath, content);
  }
}
