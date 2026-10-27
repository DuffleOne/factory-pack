-- Records where fluids meet each crafting machine, recipe by recipe. The
-- prototypes say which fluid boxes a machine has, but which box a recipe's
-- fluids land in is the engine's call once the recipe is set, so this sets
-- each one on a machine facing north and reads the connections back.

local function uses_fluid(recipe)
  for _, list in pairs({ recipe.ingredients, recipe.products }) do
    for _, part in pairs(list) do
      if part.type == "fluid" then return true end
    end
  end
  return false
end

script.on_init(function()
  local surface = game.create_surface("factory-pack")
  surface.generate_with_lab_tiles = true
  surface.request_to_generate_chunks({ 0, 0 }, 2)
  surface.force_generate_chunk_requests()
  local force = game.forces.player
  force.research_all_technologies()

  local machines = {}
  for name, proto in pairs(prototypes.entity) do
    if (proto.type == "assembling-machine" or proto.type == "rocket-silo") and #proto.fluidbox_prototypes > 0 then
      local recipes = {}
      for category in pairs(proto.crafting_categories or {}) do
        for rname, recipe in pairs(prototypes.get_recipe_filtered({ { filter = "category", category = category } })) do
          if recipes[rname] == nil and uses_fluid(recipe) then
            local ok, entity = pcall(surface.create_entity, { name = name, position = { 0, 0 }, force = force, direction = defines.direction.north })
            if ok and entity then
              force.recipes[rname].enabled = true
              if pcall(entity.set_recipe, rname) and entity.get_recipe() and entity.get_recipe().name == rname then
                local connections = {}
                for box = 1, #entity.fluidbox do
                  local fluid = entity.fluidbox.get_filter(box) or entity.fluidbox.get_locked_fluid(box)
                  if type(fluid) == "table" then fluid = fluid.name end
                  for _, pc in pairs(entity.fluidbox.get_pipe_connections(box)) do
                    connections[#connections + 1] = {
                      box = box, fluid = fluid, flow = pc.flow_direction,
                      x = pc.position.x - entity.position.x, y = pc.position.y - entity.position.y,
                      tx = pc.target_position.x - entity.position.x, ty = pc.target_position.y - entity.position.y,
                    }
                  end
                end
                recipes[rname] = connections
              end
              entity.destroy()
            end
          end
        end
      end
      if next(recipes) then machines[name] = recipes end
    end
  end
  helpers.write_file("factory-pack-fluidboxes.json", helpers.table_to_json({ format = 1, machines = machines }))
end)
