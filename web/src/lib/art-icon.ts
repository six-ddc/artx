// <art-icon name="rocket"></art-icon> — inline icons for server-rendered
// markdown (R1: the HTML arrives pre-rendered, so icons must work without
// React). A web component upgrades in place whenever MdCanvas swaps
// innerHTML; icons are bundled (lucide, tree-shaken) so this works offline.
//
// Authoring conventions (documented in blueprint.md):
//   - Always write the explicit closing tag: custom elements never
//     self-close in HTML, `<art-icon name="x"/>` swallows what follows.
//   - Icons are decorative. They carry no source text, so a comment
//     selection spanning an icon degrades to approximate anchoring — never
//     let an icon replace a load-bearing word.
import {
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  Bell,
  BookOpen,
  Bookmark,
  Bug,
  Calendar,
  Check,
  ChevronRight,
  CircleAlert,
  CircleCheck,
  CircleHelp,
  CircleX,
  Clipboard,
  Clock,
  Cloud,
  Code,
  Copy,
  Cpu,
  Database,
  Download,
  ExternalLink,
  Eye,
  File,
  FileText,
  Filter,
  Flame,
  Folder,
  GitBranch,
  GitCommitHorizontal,
  GitMerge,
  GitPullRequest,
  Globe,
  HardDrive,
  Heart,
  House,
  Image,
  Info,
  Key,
  Layers,
  Lightbulb,
  Link,
  List,
  Lock,
  LockOpen,
  Mail,
  MapPin,
  MessageSquare,
  Minus,
  Moon,
  OctagonAlert,
  Package,
  Pause,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Rocket,
  Search,
  Send,
  Server,
  Settings,
  Shield,
  Sparkles,
  Star,
  Sun,
  Table,
  Tag,
  Terminal,
  ThumbsDown,
  ThumbsUp,
  Trash2,
  TriangleAlert,
  Upload,
  User,
  Users,
  Wifi,
  Wrench,
  X,
  Zap,
  createElement,
  type IconNode,
} from 'lucide';

const ICONS: Record<string, IconNode> = {
  'arrow-down': ArrowDown,
  'arrow-left': ArrowLeft,
  'arrow-right': ArrowRight,
  'arrow-up': ArrowUp,
  bell: Bell,
  'book-open': BookOpen,
  bookmark: Bookmark,
  bug: Bug,
  calendar: Calendar,
  check: Check,
  'chevron-right': ChevronRight,
  'circle-alert': CircleAlert,
  'circle-check': CircleCheck,
  'circle-help': CircleHelp,
  'circle-x': CircleX,
  clipboard: Clipboard,
  clock: Clock,
  cloud: Cloud,
  code: Code,
  copy: Copy,
  cpu: Cpu,
  database: Database,
  download: Download,
  'external-link': ExternalLink,
  eye: Eye,
  file: File,
  'file-text': FileText,
  filter: Filter,
  flame: Flame,
  folder: Folder,
  'git-branch': GitBranch,
  'git-commit': GitCommitHorizontal,
  'git-merge': GitMerge,
  'git-pull-request': GitPullRequest,
  globe: Globe,
  'hard-drive': HardDrive,
  heart: Heart,
  home: House,
  image: Image,
  info: Info,
  key: Key,
  layers: Layers,
  lightbulb: Lightbulb,
  link: Link,
  list: List,
  lock: Lock,
  'lock-open': LockOpen,
  mail: Mail,
  'map-pin': MapPin,
  'message-square': MessageSquare,
  minus: Minus,
  moon: Moon,
  'octagon-alert': OctagonAlert,
  package: Package,
  pause: Pause,
  pencil: Pencil,
  play: Play,
  plus: Plus,
  'refresh-cw': RefreshCw,
  rocket: Rocket,
  search: Search,
  send: Send,
  server: Server,
  settings: Settings,
  shield: Shield,
  sparkles: Sparkles,
  star: Star,
  sun: Sun,
  table: Table,
  tag: Tag,
  terminal: Terminal,
  'thumbs-down': ThumbsDown,
  'thumbs-up': ThumbsUp,
  trash: Trash2,
  'triangle-alert': TriangleAlert,
  upload: Upload,
  user: User,
  users: Users,
  wifi: Wifi,
  wrench: Wrench,
  x: X,
  zap: Zap,
};

class ArtIcon extends HTMLElement {
  static observedAttributes = ['name'];

  connectedCallback(): void {
    this.render();
  }

  attributeChangedCallback(): void {
    if (this.isConnected) this.render();
  }

  private render(): void {
    // Self-heal the `<art-icon .../>` authoring mistake: a self-closed
    // custom element stays open in the HTML parser, so following content
    // lands inside it. Move any such content back out before rendering,
    // instead of silently deleting it.
    const stray = Array.from(this.childNodes).filter((n) => !(n instanceof SVGElement));
    if (stray.length > 0) this.after(...stray);

    const name = this.getAttribute('name') ?? '';
    const icon = ICONS[name];
    const svg = createElement(icon ?? CircleHelp);
    svg.setAttribute('aria-hidden', 'true');
    if (!icon) this.title = `unknown icon: ${name}`;
    this.replaceChildren(svg);
  }
}

if (!customElements.get('art-icon')) {
  customElements.define('art-icon', ArtIcon);
}
