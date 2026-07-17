import { Link } from "react-router"
import type { Project } from "@autoboard/contracts"

const ProjectList = ({ projects }: { readonly projects: readonly Project[] }) => projects.length === 0 ? <p className="empty-state">No projects</p> : (
  <ul className="project-list">
    {projects.map((project) => <li key={project.id}><Link to={`/projects/${encodeURIComponent(project.key)}`}><strong>{project.name}</strong><span>{project.key}</span></Link></li>)}
  </ul>
)

export const ProjectsPage = ({ projects }: { readonly projects: { readonly active: readonly Project[]; readonly archived: readonly Project[] } }) => (
  <section className="page projects-page">
    <div className="page-heading"><p className="eyebrow">Your work</p><h1>Projects</h1></div>
    <ProjectList projects={projects.active} />
    <section className="archived-projects"><h2>Archived projects</h2><ProjectList projects={projects.archived} /></section>
  </section>
)
